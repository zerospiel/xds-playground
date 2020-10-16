package main

import (
	"errors"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/envoyproxy/go-control-plane/pkg/cache/types"
	xds_cache "github.com/envoyproxy/go-control-plane/pkg/cache/v2"
	core "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

const meshNodeName = "mesh"

type k8sInMemoryState struct {
	snapshotCache xds_cache.SnapshotCache
	cacheEds      *xds_cache.LinearCache
	cacheCds      *xds_cache.LinearCache
	cacheRds      *xds_cache.LinearCache
	cacheLds      *xds_cache.LinearCache
	cacheAny      *xds_cache.LinearCache

	mu sync.RWMutex

	// svcState presents the whole k8s services with aligned endpoints
	// type of the field can be changed to fit your own needs
	// like k8s namespace or ports separation, matching versions of EPs
	// this one is the most simplest example
	svcState map[string][]podEndpoint
}

func newInMemoryState() *k8sInMemoryState {
	return &k8sInMemoryState{
		svcState: map[string][]podEndpoint{},
		// TODO: implement logger / use zap.Logger
		snapshotCache: xds_cache.NewSnapshotCache(true, xds_cache.IDHash{}, nil),
	}
}

func mustKubernetesClient() *kubernetes.Clientset {
	c, err := rest.InClusterConfig()
	if err != nil {
		panic(fmt.Errorf("failed get k8s cluster config: %w", err))
	}

	// formally, it can be a set of clusters
	// check it out: https://github.com/envoyproxy/go-control-plane/blob/master/examples/dyplomat/bootstrap.go#L24
	// in this case we use single separate k8s cluster
	clientSet, err := kubernetes.NewForConfig(c)
	if err != nil {
		panic(fmt.Errorf("failed init k8s client set with cluster config: %w", err))
	}

	return clientSet
}

func initInformers(cli kubernetes.Interface, rp time.Duration, stopC <-chan struct{}) (cache.SharedIndexInformer, error) {
	factory := informers.NewSharedInformerFactoryWithOptions(cli, rp)
	epsInformer := factory.Core().V1().Endpoints().Informer()

	go epsInformer.Run(stopC)

	if !cache.WaitForCacheSync(stopC, epsInformer.HasSynced) {
		return nil, errors.New("timed out waiting for endpoints shared informer to sync caches before populate them")
	}

	return epsInformer, nil
}

func (c *k8sInMemoryState) onUpdate(oldObj, newObj interface{}) {
	c.onEventProcess(newObj, "update")
}

func (c *k8sInMemoryState) onDelete(obj interface{}) {
	c.onEventProcess(obj, "delete")
}

func (c *k8sInMemoryState) onAdd(obj interface{}) {
	c.onEventProcess(obj, "add")
}

func (c *k8sInMemoryState) onUpdateC(oldObj, newObj interface{}) {
	c.onEventProcess(newObj, "update")
}

func (c *k8sInMemoryState) onDeleteC(obj interface{}) {
	c.onEventProcess(obj, "delete")
}

func (c *k8sInMemoryState) onAddC(obj interface{}) {
	c.onEventProcess(obj, "add")
}

func (c *k8sInMemoryState) onEventProcessC(obj interface{}, eventType string) {
	endpoints, ok := obj.(*core.Endpoints)
	if !ok || endpoints == nil {
		return
	}

	svcName, podEndpoints := endpoints.GetObjectMeta().GetName(), extractDataFromEndpoints(endpoints)

	// sanity check
	c.mu.RLock()
	if _, ok := c.svcState[svcName]; !ok {
		c.mu.RUnlock()
		return
	}
	c.mu.RUnlock()

	c.mu.Lock()
	c.svcState[svcName] = podEndpoints
	c.mu.Unlock()

	// NOTE: in this method we can extract locality data from endpoint in some way
	c.mu.RLock()
	var (
		eds, cds, rds, lds []types.Resource
	)
	for svc, eps := range c.svcState {
		eds = append(eds, getEDS(eps, map[string]string{zoneNameHC: regionNameHC})...)
		cds = append(cds, getCDS()...)

		svcRouteConfigName := svc + "-route-config"
		svcListenerName := svc + "-listener"

		rds = append(rds, getRDS(svcRouteConfigName, svc+"-vh", svcListenerName)...)
		v, err := getLDS(svcRouteConfigName, svcListenerName)
		if err != nil {
			c.mu.RUnlock()
			log.Printf("failed to get snapshot in %s event: %s\n", eventType, err.Error())
			return
		}
		lds = append(lds, v...)
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.cacheAny.UpdateResource("eds", eds[0]); err != nil {
		log.Printf("failed to update EDS resource type in %s event: %s\n", eventType, err.Error())
	}
	if err := c.cacheAny.UpdateResource("cds", cds[0]); err != nil {
		log.Printf("failed to update CDS resource type in %s event: %s\n", eventType, err.Error())
	}
	if err := c.cacheAny.UpdateResource("rds", rds[0]); err != nil {
		log.Printf("failed to update RDS resource type in %s event: %s\n", eventType, err.Error())
	}
	if err := c.cacheAny.UpdateResource("lds", lds[0]); err != nil {
		log.Printf("failed to update LDS resource type in %s event: %s\n", eventType, err.Error())
	}
}

func (c *k8sInMemoryState) onEventProcess(obj interface{}, eventType string) {
	endpoints, ok := obj.(*core.Endpoints)
	if !ok || endpoints == nil {
		return
	}

	svcName, podEndpoints := endpoints.GetObjectMeta().GetName(), extractDataFromEndpoints(endpoints)

	// sanity check
	c.mu.RLock()
	if _, ok := c.svcState[svcName]; !ok {
		c.mu.RUnlock()
		return
	}
	c.mu.RUnlock()

	c.mu.Lock()
	c.svcState[svcName] = podEndpoints
	c.mu.Unlock()

	// NOTE: in this method we can extract locality data from endpoint in some way
	c.mu.RLock()
	s, err := getSnapshot(c.svcState, map[string]string{
		zoneNameHC: regionNameHC,
	})
	if err != nil {
		c.mu.RUnlock()
		log.Printf("failed to get snapshot in %s event: %s\n", eventType, err.Error())
		return
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()
	if err = c.snapshotCache.SetSnapshot(meshNodeName, s); err != nil {
		log.Printf("failed to set snapshot in %s event: %s\n", eventType, err.Error())
	}
	atomic.AddUint64(&snapshotVersion, 1)
}

func extractDataFromEndpoints(endpoints *core.Endpoints) (podEndpoints []podEndpoint) {
	for _, subset := range endpoints.Subsets {
		for _, p := range subset.Ports {
			for _, a := range subset.Addresses {
				podEndpoints = append(podEndpoints, podEndpoint{port: p.Port, ip: a.IP})
			}
		}
	}

	return
}
