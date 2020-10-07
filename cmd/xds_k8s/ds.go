package main

import (
	"errors"

	api "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	envoy_core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	ep "github.com/envoyproxy/go-control-plane/envoy/api/v2/endpoint"
	route "github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	"github.com/envoyproxy/go-control-plane/pkg/cache/types"
	xds_cache "github.com/envoyproxy/go-control-plane/pkg/cache/v2"
	"google.golang.org/protobuf/types/known/wrapperspb"
	core "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
)

// those are some hardcoded values
// it won't be used in that way on production
const (
	clusterNameHC = "cluster_name"  // is it enough to be just k8s cluster name?
	regionNameHC  = "tdb_hardcoded" // i don't know in which case it may be used tbh
	zoneNameHC    = "dataline_dc"   // this is some datacenter name
)

type (
	podEndpoint struct {
		ip   string
		port int32
	}
)

func initSnapshot(epsInformer cache.SharedIndexInformer, services ...string) (xds_cache.Snapshot, error) {
	// current example has single service
	svcSet := make(map[string]struct{}, len(services))
	for _, svcName := range services {
		svcSet[svcName] = struct{}{}
	}

	svc2Eps := make(map[string][]podEndpoint)

	for _, obj := range epsInformer.GetStore().List() {
		endpoint := obj.(*core.Endpoints)
		if endpoint == nil {
			return xds_cache.Snapshot{}, errors.New("endpoints object expected")
		}

		epSvcName := endpoint.GetObjectMeta().GetName()
		if _, ok := svcSet[epSvcName]; !ok {
			continue
		}

		var podEps []podEndpoint
		for _, subset := range endpoint.Subsets {
			for _, p := range subset.Ports {
				for _, a := range subset.Addresses {
					podEps = append(podEps, podEndpoint{port: p.Port, ip: a.IP})
				}
			}
		}

		svc2Eps[epSvcName] = podEps
	}

	// TODO: save initial k8s endpoints state
	// generate initial snapshot
	// on update/add/delete update k8s endpoints state
	// regenerate the whole snapshot again

	return xds_cache.Snapshot{}, nil
}

// getLDS creates Listener resources.
// LDS returns Listener resources.
// Used basically as a convenient root for
//  the gRPC client's configuration.
// Points to the RouteConfiguration.
func getLDS() []types.Resource {
	return nil
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
