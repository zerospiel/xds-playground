package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	api "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	ep "github.com/envoyproxy/go-control-plane/envoy/api/v2/endpoint"
	api_listener "github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	route "github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	hcm "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v2"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	"github.com/envoyproxy/go-control-plane/pkg/cache/types"
	"github.com/envoyproxy/go-control-plane/pkg/cache/v2"
	xds "github.com/envoyproxy/go-control-plane/pkg/server/v2"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"github.com/golang/protobuf/ptypes"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

type upstreamPorts []int

func (i *upstreamPorts) String() string {
	return strings.Join(strings.Fields(fmt.Sprint(*i)), ",")
}

func (i *upstreamPorts) Set(value string) error {
	log.Printf("[upstream ports] %s", value)
	v, err := strconv.Atoi(value)
	if err != nil {
		return err
	}
	*i = append(*i, v)
	return nil
}

// some hardcoded hosts and names
const (
	someClusterName     = "some_svc_cluster_name"
	someVHName          = "some_svc_vh_name"
	someRouteConfigName = "some_svc_route_config_name"
	someListenerName    = "warden.platform" // initial grpc service we trying to send request to via grpc conn
	someEndpointAddress = "0.0.0.0"         // backend, grpc server we trying to send request to via lb

	sleepTime = time.Second * 3
)

var (
	mgmtPort  int
	upstreams upstreamPorts
	live      time.Duration

	snapshotVersion uint32
)

func init() {
	flag.IntVar(&mgmtPort, "port", 18000, "management server port")
	flag.Var(&upstreams, "upstream_port", "list of upstream grpc servers ports, must be set at least one")
	flag.DurationVar(&live, "uptime", time.Minute, "how much time keep xds server before terminating it")
}

func main() {
	flag.Parse()

	defer func() {
		if r := recover(); r != nil {
			log.Println("got panic", r)
			os.Exit(129)
		}
	}()

	if len(upstreams) == 0 {
		flag.Usage()
		os.Exit(128)
	}

	signal := make(chan struct{})
	cb := &callbacks{
		signal:   signal,
		fetches:  0,
		requests: 0,
	}

	ctx, cancel := context.WithCancel(context.Background())

	snapshotCache := cache.NewSnapshotCache(true, cache.IDHash{}, nil)
	xdsServer := xds.NewServer(ctx, snapshotCache, cb)

	go runMgmtServer(ctx, xdsServer, mgmtPort)

	<-signal
	go func() {
		<-time.After(live)
		cancel()
	}()

	cb.Report()

	nodeID := snapshotCache.GetStatusKeys()[0]
	log.Println("got nodeID", nodeID)

	go func() {
		var (
			l = len(upstreams)
			i int
		)
		for {
			upstreamPort := upstreams[i%l] // emulate updating/adding endpoints for local :)

			// eds
			log.Printf("creating ENDPOINT for %s:%d\n", someEndpointAddress, upstreamPort)
			eds := getEDS(someEndpointAddress, uint32(upstreamPort), map[string]string{"my-region": "my-zone"})

			// cds
			log.Printf("creating CLUSTER %s\n", someClusterName)
			cds := getCDS()

			// rds
			log.Printf("creating ROUTE %s\n", someRouteConfigName)
			rds := getRDS(someRouteConfigName, someVHName, someListenerName)

			// lds
			log.Printf("creating LISTENER %s\n", someListenerName)
			lst, err := getLDS(someRouteConfigName, someListenerName)
			if err != nil {
				log.Fatalf("failed get LDS: %s\n", err.Error())
			}

			atomic.AddUint32(&snapshotVersion, 1)
			log.Printf("creating snapshot with version %d\n", snapshotVersion)

			ss := cache.NewSnapshot(fmt.Sprint(snapshotVersion), eds, cds, rds, lst, nil, nil)
			if err = ss.Consistent(); err != nil {
				log.Fatal(err)
			}
			snapshotCache.SetSnapshot(nodeID, ss)

			i++
			// just to keep snapshot alive
			log.Println("sleeping ...", sleepTime)
			time.Sleep(sleepTime)
		}
	}()
	<-ctx.Done()
}

func runMgmtServer(ctx context.Context, xdsServer xds.Server, port int) {
	grpcServer := grpc.NewServer(grpc.MaxConcurrentStreams(1000))
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Fatalf("failed to listen to :%d: %s", port, err.Error())
	}

	discovery.RegisterAggregatedDiscoveryServiceServer(grpcServer, xdsServer)
	api.RegisterEndpointDiscoveryServiceServer(grpcServer, xdsServer)
	api.RegisterClusterDiscoveryServiceServer(grpcServer, xdsServer)
	api.RegisterRouteDiscoveryServiceServer(grpcServer, xdsServer)
	api.RegisterListenerDiscoveryServiceServer(grpcServer, xdsServer)

	if err = grpcServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve grpc server on :%d: %s", port, err.Error())
	}

	<-ctx.Done()
	shutdown(grpcServer)
}

func shutdown(server *grpc.Server) {
	if server == nil {
		return
	}
	done := make(chan struct{})
	go func() {
		server.GracefulStop()
		close(done)
	}()

	select {
	case <-done:
		log.Println("gracefully shutdown server ...")
	case <-time.After(time.Second):
		log.Println("timeout for gracefull shutdown, stoping ...")
		server.Stop()
	}
}

// getLDS creates Listener resources.
// LDS returns Listener resources.
// Used basically as a convenient root for
// the gRPC client's configuration.
// Points to the RouteConfiguration.
func getLDS(svcRouteConfigName, svcListenerName string) ([]types.Resource, error) {
	httpConnRds := &hcm.HttpConnectionManager_Rds{
		Rds: &hcm.Rds{
			RouteConfigName: svcRouteConfigName,
			ConfigSource: &core.ConfigSource{
				ConfigSourceSpecifier: &core.ConfigSource_Ads{
					Ads: &core.AggregatedConfigSource{},
				},
			},
		},
	}

	httpConnManager := &hcm.HttpConnectionManager{
		CodecType:      hcm.HttpConnectionManager_AUTO,
		RouteSpecifier: httpConnRds,
	}

	anyListener, err := ptypes.MarshalAny(httpConnManager)
	if err != nil {
		return nil, fmt.Errorf("failed marshal any proto: %w", err)
	}

	return []types.Resource{
		&api.Listener{
			Name: svcListenerName,
			ApiListener: &listener.ApiListener{
				ApiListener: anyListener,
			},
			FilterChains: []*api_listener.FilterChain{
				{
					Filters: []*api_listener.Filter{
						{
							Name: wellknown.HTTPConnectionManager,
							ConfigType: &api_listener.Filter_TypedConfig{
								TypedConfig: anyListener,
							},
						},
					},
				},
			},
		},
	}, nil
}

// getRDS creates RouteConfiguration resources.
// RDS returns RouteConfiguration resources.
// Provides data used to populate the gRPC service config.
// Points to the Cluster.
func getRDS(svcRouteConfigName, svcVirtualHostName, svcListenerName string) []types.Resource {
	vhost := &route.VirtualHost{
		Name:    svcVirtualHostName,
		Domains: []string{svcListenerName},
		Routes: []*route.Route{{
			Match: &route.RouteMatch{
				PathSpecifier: &route.RouteMatch_Prefix{
					Prefix: "",
				},
			},
			Action: &route.Route_Route{Route: &route.RouteAction{
				ClusterSpecifier: &route.RouteAction_Cluster{
					Cluster: someClusterName,
				}}},
		}},
	}

	return []types.Resource{
		&api.RouteConfiguration{
			Name:         svcRouteConfigName,
			VirtualHosts: []*route.VirtualHost{vhost},
		},
	}
}

// getCDS creates Cluster resources.
// CDS returns Cluster resources. Configures things like
// load balancing policy and load reporting.
// Points to the ClusterLoadAssignment.
func getCDS() []types.Resource {
	return []types.Resource{
		&api.Cluster{
			Name:                 someClusterName,
			LbPolicy:             api.Cluster_ROUND_ROBIN,                  // as of grpc-go 1.32.x it's the only option
			ClusterDiscoveryType: &api.Cluster_Type{Type: api.Cluster_EDS}, // points to EDS
			EdsClusterConfig: &api.Cluster_EdsClusterConfig{
				EdsConfig: &core.ConfigSource{
					ConfigSourceSpecifier: &core.ConfigSource_Ads{}, // as of grpc-go 1.32.x it's the only option for DS config source
					InitialFetchTimeout:   &durationpb.Duration{Seconds: 0, Nanos: 0},
				},
			},
		},
	}
}

// getEDS creates ClusterLoadAssignment resources.
// EDS returns ClusterLoadAssignment resources.
// Configures the set of endpoints (backend servers) to
// load balance across and may tell the client to drop requests.
func getEDS(ip string, port uint32, localitiesZone2Reg map[string]string) []types.Resource {
	var lbeps []*ep.LbEndpoint
	hst := &core.Address{
		Address: &core.Address_SocketAddress{
			SocketAddress: &core.SocketAddress{
				Address:  ip,
				Protocol: core.SocketAddress_TCP,
				PortSpecifier: &core.SocketAddress_PortValue{
					PortValue: uint32(port),
				},
			},
		},
	}

	lbeps = append(lbeps, &ep.LbEndpoint{
		HostIdentifier: &ep.LbEndpoint_Endpoint{
			Endpoint: &ep.Endpoint{
				Address: hst,
			},
		},
		HealthStatus: core.HealthStatus_HEALTHY,
	})

	var localityLbEps []*ep.LocalityLbEndpoints
	for zone, region := range localitiesZone2Reg {
		localityLbEps = append(localityLbEps, &ep.LocalityLbEndpoints{
			Priority: 0, // highest priority, read desc for more
			LoadBalancingWeight: &wrapperspb.UInt32Value{
				Value: uint32(1000),
			},
			LbEndpoints: lbeps,
			Locality: &core.Locality{
				Zone:   zone,
				Region: region,
			},
		})
	}

	return []types.Resource{
		&api.ClusterLoadAssignment{
			ClusterName: someClusterName,
			Endpoints:   localityLbEps,
		},
	}
}
