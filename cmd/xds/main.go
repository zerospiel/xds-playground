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
	route "github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	hcm "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v2"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	"github.com/envoyproxy/go-control-plane/pkg/cache/types"
	"github.com/envoyproxy/go-control-plane/pkg/cache/v2"
	xds "github.com/envoyproxy/go-control-plane/pkg/server/v2"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/wrappers"
	"google.golang.org/grpc"
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
	mgmtPort, gtwPort int
	upstreams         upstreamPorts
	live              time.Duration

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
			hst := &core.Address{
				Address: &core.Address_SocketAddress{
					SocketAddress: &core.SocketAddress{
						Address:  someEndpointAddress,
						Protocol: core.SocketAddress_TCP,
						PortSpecifier: &core.SocketAddress_PortValue{
							PortValue: uint32(upstreamPort),
						},
					},
				},
			}

			eds := []types.Resource{
				&api.ClusterLoadAssignment{
					ClusterName: someClusterName,
					Endpoints: []*ep.LocalityLbEndpoints{
						{
							Locality: &core.Locality{
								Region: "my-region",
								Zone:   "my-zone",
							},
							Priority:            0,
							LoadBalancingWeight: &wrappers.UInt32Value{Value: uint32(1000)},
							LbEndpoints: []*ep.LbEndpoint{
								{
									HostIdentifier: &ep.LbEndpoint_Endpoint{
										Endpoint: &ep.Endpoint{
											Address: hst,
										},
									},
									HealthStatus: core.HealthStatus_HEALTHY,
								},
							},
						},
					},
				},
			}

			// cds
			log.Printf("creating CLUSTER %s\n", someClusterName)
			cds := []types.Resource{
				&api.Cluster{
					Name:     someClusterName,
					LbPolicy: api.Cluster_ROUND_ROBIN,
					ClusterDiscoveryType: &api.Cluster_Type{
						Type: api.Cluster_EDS,
					},
					EdsClusterConfig: &api.Cluster_EdsClusterConfig{
						EdsConfig: &core.ConfigSource{
							ConfigSourceSpecifier: &core.ConfigSource_Ads{},
						},
					},
				},
			}

			// rds
			log.Printf("creating ROUTE %s\n", someRouteConfigName)
			rds := []types.Resource{
				&api.RouteConfiguration{
					Name: someRouteConfigName,
					VirtualHosts: []*route.VirtualHost{
						{
							Name:    someVHName,
							Domains: []string{someListenerName},
							Routes: []*route.Route{
								{
									Match: &route.RouteMatch{
										PathSpecifier: &route.RouteMatch_Prefix{Prefix: ""},
									},
									Action: &route.Route_Route{
										Route: &route.RouteAction{
											ClusterSpecifier: &route.RouteAction_Cluster{
												Cluster: someClusterName,
											},
										},
									},
								},
							},
						},
					},
				},
			}

			// lds
			log.Printf("creating LISTENER %s\n", someListenerName)
			httpConnRds := &hcm.HttpConnectionManager_Rds{
				Rds: &hcm.Rds{
					RouteConfigName: someRouteConfigName,
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
				log.Fatalf("on marshal proto: %s\n", err.Error())
			}

			lst := []types.Resource{
				&api.Listener{
					Name: someListenerName,
					ApiListener: &listener.ApiListener{
						ApiListener: anyListener,
					},
				},
			}

			atomic.AddUint32(&snapshotVersion, 1)
			log.Printf("creating snapshot with version %d\n", snapshotVersion)

			ss := cache.NewSnapshot(fmt.Sprint(snapshotVersion), eds, cds, rds, lst, nil, nil)
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
