package main

import (
	"errors"
	"fmt"
	"sync"
	"time"

	xds_cache "github.com/envoyproxy/go-control-plane/pkg/cache/v2"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

type eventType int

const (
	eventAdd eventType = iota
	eventUpdate
	eventDelete
)

type k8sInMemoryState struct {
	snapshotCache xds_cache.SnapshotCache

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
	c.onEvent(oldObj, newObj, eventAdd)
}

func (c *k8sInMemoryState) onDelete(obj interface{}) {
	c.onEvent(obj, nil, eventDelete)
}

func (c *k8sInMemoryState) onAdd(obj interface{}) {
	c.onEvent(nil, obj, eventAdd)
}

func (c *k8sInMemoryState) onEvent(oldObj, newObj interface{}, eventType eventType) {

}
