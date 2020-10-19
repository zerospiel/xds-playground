package main

import (
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/envoyproxy/go-control-plane/pkg/cache/types"
	xds_cache "github.com/envoyproxy/go-control-plane/pkg/cache/v2"
	core "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

type k8sInMemoryState struct {
	// muxCache is a combinator for other resources caches
	muxCache *xds_cache.MuxCache

	lcacheEds *xds_cache.LinearCache
	lcacheCds *xds_cache.LinearCache
	lcacheRds *xds_cache.LinearCache
	lcacheLds *xds_cache.LinearCache

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
	// TODO: here should be a separate method for incremental resources
	// but envoy currently doesn't support incremental DS
	eds, cds, rds, lds, err := getResources(c.svcState, map[string]string{zoneNameHC: regionNameHC})
	if err != nil {
		c.mu.RUnlock()
		log.Println(err.Error())
		return
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	log.Printf("updating caches:\nEDS: %+v\nCDS: %+v\nRDS: %+v\nLDS: %+v\n\n\n", eds, cds, rds, lds)

	if err := updateResource(eds, c.lcacheEds); err != nil {
		log.Printf("failed to update EDS resource type in %s event: %s\n", eventType, err.Error())
		return
	}
	if err := updateResource(cds, c.lcacheCds); err != nil {
		log.Printf("failed to update CDS resource type in %s event: %s\n", eventType, err.Error())
		return
	}
	if err := updateResource(rds, c.lcacheRds); err != nil {
		log.Printf("failed to update RDS resource type in %s event: %s\n", eventType, err.Error())
		return
	}
	if err := updateResource(lds, c.lcacheLds); err != nil {
		log.Printf("failed to update LDS resource type in %s event: %s\n", eventType, err.Error())
		return
	}
}

func updateResource(resources map[string]types.Resource, cacheType *xds_cache.LinearCache) error {
	for resourceName, resource := range resources {
		if err := cacheType.UpdateResource(resourceName, resource); err != nil {
			return err
		}
	}

	return nil
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
