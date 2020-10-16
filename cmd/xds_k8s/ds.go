package main

import (
	"errors"
	"fmt"
	"log"
	"sync/atomic"

	api "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	envoy_core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	ep "github.com/envoyproxy/go-control-plane/envoy/api/v2/endpoint"
	api_listener "github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	route "github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	hcm "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v2"
	"github.com/envoyproxy/go-control-plane/pkg/cache/types"
	xds_cache "github.com/envoyproxy/go-control-plane/pkg/cache/v2"
	resource "github.com/envoyproxy/go-control-plane/pkg/resource/v2"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"github.com/golang/protobuf/ptypes"
	"google.golang.org/protobuf/types/known/wrapperspb"
	core "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
)

// NOTE: funcs for getting DS resources may change signature to accept
// cluster name, this will be useful when you have multiple
// clusters and want to balance btwn them,
// especially that it's already implemented since grpc-go 1.31.0

// those are some HardCoded values
// it won't be used in that way on production
const (
	clusterNameHC = "cluster_name"  // is it enough to be just k8s cluster name?
	regionNameHC  = "tdb_hardcoded" // i don't know in which case it may be used tbh
	zoneNameHC    = "dataline_dc"   // this is some datacenter name

	serviceName = "backend" // some hardcoded svc name, for sake of productivity
)

type (
	podEndpoint struct {
		ip   string
		port int32
	}
)

func (c *k8sInMemoryState) initState(epsInformer cache.SharedIndexInformer) error {
	svc2Eps := make(map[string][]podEndpoint)

	for _, obj := range epsInformer.GetStore().List() {
		endpoint := obj.(*core.Endpoints)
		if endpoint == nil {
			return errors.New("endpoints object expected")
		}

		epSvcName := endpoint.GetObjectMeta().GetName()
		if _, ok := svc2Eps[epSvcName]; ok || epSvcName != serviceName {
			continue
		}

		svc2Eps[epSvcName] = extractDataFromEndpoints(endpoint)
	}

	c.svcState = svc2Eps
	// s, err := getSnapshot(c.svcState, map[string]string{
	// 	zoneNameHC: regionNameHC,
	// })
	// if err != nil {
	// 	return fmt.Errorf("failed to get snapshot: %w", err)
	// }
	// if err = c.snapshotCache.SetSnapshot(meshNodeName, s); err != nil {
	// 	return fmt.Errorf("failed to set snapshot: %w", err)
	// }

	var (
		eds, cds, rds, lds []types.Resource
	)
	for svc, eps := range svc2Eps {
		eds = append(eds, getEDS(eps, map[string]string{zoneNameHC: regionNameHC})...)
		cds = append(cds, getCDS()...)

		svcRouteConfigName := svc + "-route-config"
		svcListenerName := svc + "-listener"

		rds = append(rds, getRDS(svcRouteConfigName, svc+"-vh", svcListenerName)...)
		v, err := getLDS(svcRouteConfigName, svcListenerName)
		if err != nil {
			panic(fmt.Errorf("failed getting lds for '%s': %w", svc, err))
		}
		lds = append(lds, v...)
	}

	// c.cacheEds = xds_cache.NewLinearCache(resource.EndpointType, xds_cache.WithInitialResources(map[string]types.Resource{serviceName: eds[0]}))
	// c.cacheCds = xds_cache.NewLinearCache(resource.ClusterType, xds_cache.WithInitialResources(map[string]types.Resource{serviceName: cds[0]}))
	// c.cacheRds = xds_cache.NewLinearCache(resource.RouteType, xds_cache.WithInitialResources(map[string]types.Resource{serviceName: rds[0]}))
	// c.cacheLds = xds_cache.NewLinearCache(resource.ListenerType, xds_cache.WithInitialResources(map[string]types.Resource{serviceName: lds[0]}))
	if len(svc2Eps) > 0 {
		log.Printf("setting new cache: %+v %+v %+v %+v\n", eds[0], cds[0], rds[0], lds[0])
		c.cacheAny = xds_cache.NewLinearCache(resource.AnyType, xds_cache.WithInitialResources(map[string]types.Resource{
			"eds": eds[0],
			"cds": cds[0],
			"rds": rds[0],
			"lds": lds[0],
		}))
	}

	epsInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.onAddC,
		UpdateFunc: c.onUpdateC,
		DeleteFunc: c.onDeleteC,
	})

	// epsInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
	// 	AddFunc:    c.onAdd,
	// 	UpdateFunc: c.onUpdate,
	// 	DeleteFunc: c.onDelete,
	// })

	return nil
}

func getSnapshot(svc2Eps map[string][]podEndpoint, localitiesZone2Reg map[string]string) (xds_cache.Snapshot, error) {
	return xds_cache.Snapshot{}, nil
	var (
		eds, cds, rds, lds []types.Resource
	)
	for svc, eps := range svc2Eps {
		eds = append(eds, getEDS(eps, localitiesZone2Reg)...)
		cds = append(cds, getCDS()...)

		svcRouteConfigName := svc + "-route-config"
		svcListenerName := svc + "-listener"

		rds = append(rds, getRDS(svcRouteConfigName, svc+"-vh", svcListenerName)...)
		v, err := getLDS(svcRouteConfigName, svcListenerName)
		if err != nil {
			return xds_cache.Snapshot{}, fmt.Errorf("failed getting lds for '%s': %w", svc, err)
		}
		lds = append(lds, v...)
	}

	s := xds_cache.NewSnapshot(fmt.Sprint(snapshotVersion), eds, cds, rds, lds, nil, nil)
	log.Println("setting new snapshot", eds, cds, rds, lds)
	if err := s.Consistent(); err != nil {
		return xds_cache.Snapshot{}, fmt.Errorf("inconsistent snapshot version %d: %w", snapshotVersion, err)
	}

	atomic.AddUint64(&snapshotVersion, 1)
	return s, nil
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
			ConfigSource: &envoy_core.ConfigSource{
				ConfigSourceSpecifier: &envoy_core.ConfigSource_Ads{
					Ads: &envoy_core.AggregatedConfigSource{},
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
			FilterChains: []*api_listener.FilterChain{{
				Filters: []*api_listener.Filter{{
					Name: wellknown.HTTPConnectionManager,
					ConfigType: &api_listener.Filter_TypedConfig{
						TypedConfig: anyListener,
					},
				}},
			}},
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
				// since grpc-go 1.31.0 there is possibility for requests
				// matching based on path (prefix, full path and safe regex)
				// and headers (check out route.HeaderMatcher)
				PathSpecifier: &route.RouteMatch_Prefix{Prefix: ""},
				// PathSpecifier: &route.RouteMatch_SafeRegex{},
			},
			Action: &route.Route_Route{Route: &route.RouteAction{
				// singce grpc-go 1.31.0 supports weighted clusters
				// there is a possibility to specify ClusterSpecifier
				// with one of: Cluster or WeightedCluster (ClusterHeader still unavailable)

				// cluster example
				// ClusterSpecifier: &route.RouteAction_Cluster{
				// 	Cluster: clusterNameHC,
				// }}},

				// weighted cluster example
				ClusterSpecifier: &route.RouteAction_WeightedClusters{
					WeightedClusters: &route.WeightedCluster{
						TotalWeight: &wrapperspb.UInt32Value{Value: uint32(100)}, // default value
						Clusters: []*route.WeightedCluster_ClusterWeight{
							{
								Name:   clusterNameHC,
								Weight: &wrapperspb.UInt32Value{Value: uint32(100)}, // since we have only one k8s cluster
							},
						},
					},
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
			Name:                 clusterNameHC,
			LbPolicy:             api.Cluster_ROUND_ROBIN,                  // as of grpc-go 1.32.x it's the only option
			ClusterDiscoveryType: &api.Cluster_Type{Type: api.Cluster_EDS}, // points to EDS
			EdsClusterConfig: &api.Cluster_EdsClusterConfig{
				EdsConfig: &envoy_core.ConfigSource{
					ConfigSourceSpecifier: &envoy_core.ConfigSource_Ads{}, // as of grpc-go 1.32.x it's the only option for DS config source
				},
			},
		},
	}
}

// getEDS creates ClusterLoadAssignment resources.
// EDS returns ClusterLoadAssignment resources.
// Configures the set of endpoints (backend servers) to
// load balance across and may tell the client to drop requests.
func getEDS(endpoints []podEndpoint, localitiesZone2Reg map[string]string) []types.Resource {
	var lbeps []*ep.LbEndpoint
	for _, podEp := range endpoints {
		hst := &envoy_core.Address{
			Address: &envoy_core.Address_SocketAddress{
				SocketAddress: &envoy_core.SocketAddress{
					Address:  podEp.ip,
					Protocol: envoy_core.SocketAddress_TCP,
					PortSpecifier: &envoy_core.SocketAddress_PortValue{
						PortValue: uint32(podEp.port),
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
			HealthStatus: envoy_core.HealthStatus_HEALTHY,
		})
	}

	var localityLbEps []*ep.LocalityLbEndpoints
	for zone, region := range localitiesZone2Reg {
		localityLbEps = append(localityLbEps, &ep.LocalityLbEndpoints{
			Priority: 0, // highest priority, read desc for more
			LoadBalancingWeight: &wrapperspb.UInt32Value{
				Value: uint32(1000),
			},
			LbEndpoints: lbeps,
			Locality: &envoy_core.Locality{
				Zone:   zone,
				Region: region,
			},
		})
	}

	return []types.Resource{
		&api.ClusterLoadAssignment{
			ClusterName: clusterNameHC,
			Endpoints:   localityLbEps,
		},
	}
}
