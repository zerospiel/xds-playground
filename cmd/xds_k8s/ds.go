package main

import (
	"errors"
	"fmt"
	"log"

	api "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	envoy_core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	ep "github.com/envoyproxy/go-control-plane/envoy/api/v2/endpoint"
	route "github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	hcm "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v2"
	"github.com/envoyproxy/go-control-plane/pkg/cache/types"
	xds_cache "github.com/envoyproxy/go-control-plane/pkg/cache/v2"
	"github.com/envoyproxy/go-control-plane/pkg/resource/v2"
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
	regionNameHC = "tdb_hardcoded" // i don't know in which case it may be used tbh
	zoneNameHC   = "dataline_dc"   // this is some datacenter name

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

	eds, cds, rds, lds, err := getResources(svc2Eps, map[string]string{zoneNameHC: regionNameHC})
	if err != nil {
		panic(err)
	}

	log.Printf("setting new caches:\nEDS: %+v\nCDS: %+v\nRDS: %+v\nLDS: %+v\n\n\n", eds, cds, rds, lds)

	c.lcacheEds = xds_cache.NewLinearCache(resource.EndpointType, xds_cache.WithInitialResources(eds))
	c.lcacheCds = xds_cache.NewLinearCache(resource.ClusterType, xds_cache.WithInitialResources(cds))
	c.lcacheRds = xds_cache.NewLinearCache(resource.RouteType, xds_cache.WithInitialResources(rds))
	c.lcacheLds = xds_cache.NewLinearCache(resource.ListenerType, xds_cache.WithInitialResources(lds))

	epsInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.onAdd,
		UpdateFunc: c.onUpdate,
		DeleteFunc: c.onDelete,
	})

	c.muxCache = &xds_cache.MuxCache{
		Classify: func(request xds_cache.Request) string {
			return request.TypeUrl
		},
		Caches: map[string]xds_cache.Cache{
			resource.EndpointType: c.lcacheEds,
			resource.ClusterType:  c.lcacheCds,
			resource.RouteType:    c.lcacheRds,
			resource.ListenerType: c.lcacheLds,
		},
	}

	return nil
}

// getResources returns resources map where keys represent
// resource names to match appropriate resource type.
// Error may be only if there was a problem with marshaling protobuf any value.
func getResources(svc2Eps map[string][]podEndpoint, localitiesZone2Reg map[string]string) (eds, cds, rds, lds map[string]types.Resource, err error) {
	eds = map[string]types.Resource{}
	cds = map[string]types.Resource{}
	rds = map[string]types.Resource{}
	lds = map[string]types.Resource{}

	for svc, eps := range svc2Eps {
		eds[svc] = getEDS(svc, eps, localitiesZone2Reg)
		cds[zoneNameHC] = getCDS(svc)

		svcRouteConfigName := svc + "-route-config"

		rds[svcRouteConfigName] = getRDS(svcRouteConfigName, svc+"-vh", svc)
		v, lerr := getLDS(svcRouteConfigName, svc)
		if lerr != nil {
			err = fmt.Errorf("failed getting lds for '%s': %w", svc, lerr)
			return
		}
		lds[svc] = v
	}

	return
}

// getLDS creates Listener resources.
// LDS returns Listener resources.
// Used basically as a convenient root for
// the gRPC client's configuration.
// Points to the RouteConfiguration.
func getLDS(svcRouteConfigName, svcListenerName string) (types.Resource, error) {
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

	return types.Resource(
		&api.Listener{
			Name: svcListenerName,
			ApiListener: &listener.ApiListener{
				ApiListener: anyListener,
			},
		}), nil
}

// getRDS creates RouteConfiguration resource.
// RDS returns RouteConfiguration resource.
// Provides data used to populate the gRPC service config.
// Points to the Cluster.
func getRDS(svcRouteConfigName, svcVirtualHostName, svcListenerName string) types.Resource {
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
				// 	Cluster: zoneNameHC,
				// }}},

				// weighted cluster example
				ClusterSpecifier: &route.RouteAction_WeightedClusters{
					WeightedClusters: &route.WeightedCluster{
						TotalWeight: &wrapperspb.UInt32Value{Value: uint32(100)}, // default value
						Clusters: []*route.WeightedCluster_ClusterWeight{
							{
								Name:   zoneNameHC,
								Weight: &wrapperspb.UInt32Value{Value: uint32(100)}, // since we have only one k8s cluster
							},
						},
					},
				}}},
		}},
	}

	return types.Resource(
		&api.RouteConfiguration{
			Name:         svcRouteConfigName,
			VirtualHosts: []*route.VirtualHost{vhost},
		})
}

// getCDS creates Cluster resource.
// CDS returns Cluster resource. Configures things like
// load balancing policy and load reporting.
// Points to the ClusterLoadAssignment.
func getCDS(serviceName string) types.Resource {
	return types.Resource(
		&api.Cluster{
			Name:                 zoneNameHC,
			LbPolicy:             api.Cluster_ROUND_ROBIN,                  // as of grpc-go 1.32.x it's the only option
			ClusterDiscoveryType: &api.Cluster_Type{Type: api.Cluster_EDS}, // points to EDS
			EdsClusterConfig: &api.Cluster_EdsClusterConfig{
				EdsConfig: &envoy_core.ConfigSource{
					ConfigSourceSpecifier: &envoy_core.ConfigSource_Ads{}, // as of grpc-go 1.32.x it's the only option for DS config source
				},
				ServiceName: serviceName,
			},
			CommonLbConfig: &api.Cluster_CommonLbConfig{
				LocalityConfigSpecifier: &api.Cluster_CommonLbConfig_LocalityWeightedLbConfig_{
					LocalityWeightedLbConfig: &api.Cluster_CommonLbConfig_LocalityWeightedLbConfig{},
				},
			},
		})
}

// getEDS creates ClusterLoadAssignment resource.
// EDS returns ClusterLoadAssignment resource.
// Configures the set of endpoints (backend servers) to
// load balance across and may tell the client to drop requests.
func getEDS(serviceName string, endpoints []podEndpoint, localitiesZone2Reg map[string]string) types.Resource {
	var (
		weights = []uint32{70, 20, 0}
		lbeps   []*ep.LbEndpoint
		sumW    uint32
	)

	for i, podEp := range endpoints {
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

		w := weights[i%len(weights)]
		sumW += w

		lbeps = append(lbeps, &ep.LbEndpoint{
			HostIdentifier: &ep.LbEndpoint_Endpoint{
				Endpoint: &ep.Endpoint{
					Address: hst,
				},
			},
			HealthStatus: envoy_core.HealthStatus_HEALTHY,
			LoadBalancingWeight: &wrapperspb.UInt32Value{
				Value: w,
			},
		})
	}

	var localityLbEps []*ep.LocalityLbEndpoints
	for zone, region := range localitiesZone2Reg {
		localityLbEps = append(localityLbEps, &ep.LocalityLbEndpoints{
			Priority: 0, // highest priority, read desc for more
			LoadBalancingWeight: &wrapperspb.UInt32Value{
				Value: sumW,
			},
			LbEndpoints: lbeps,
			Locality: &envoy_core.Locality{
				Zone:   zone,
				Region: region,
			},
		})
	}

	return types.Resource(
		&api.ClusterLoadAssignment{
			ClusterName: serviceName,
			Endpoints:   localityLbEps,
		})
}
