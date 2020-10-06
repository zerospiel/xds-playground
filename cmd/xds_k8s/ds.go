package main

import (
	"errors"

	"github.com/envoyproxy/go-control-plane/pkg/cache/types"
	xds_cache "github.com/envoyproxy/go-control-plane/pkg/cache/v2"
	core "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
)

// those are some hardcoded values
// it won't be used in that way on production
const (
	clusterNameHC = "cluster_name"
	regionNameHC  = "tdb_hardcoded"
	zoneNameHC    = "dataline_dc"
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

		var (
			ports []int32
			ips   []string
		)

		for _, subset := range endpoint.Subsets {
			for _, p := range subset.Ports {
				ports = append(ports, p.Port)
			}
			for _, a := range subset.Addresses {
				ips = append(ips, a.IP)
			}
		}

		svc2Eps[epSvcName] = nil // TODO:

	}
	return xds_cache.Snapshot{}, nil
}

// getEDS creates ClusterLoadAssignment
func getEDS(endpoints []podEndpoint) (eds []types.Resource, err error) {
	return nil, nil
}
