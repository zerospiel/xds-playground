package main

import (
	"errors"
	"fmt"
	"time"

	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

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
