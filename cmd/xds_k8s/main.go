package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

var (
	mgmtPort     int
	resyncPeriod time.Duration

	snapshotVersion uint64
)

func init() {
	flag.IntVar(&mgmtPort, "port", 18000, "management server port")
	flag.DurationVar(&resyncPeriod, "sync_period", time.Hour, "k8s informers full resync period")
}

func main() {
	flag.Parse()

	defer func() {
		if r := recover(); r != nil {
			log.Println("got panic", r)
			os.Exit(129)
		}
	}()

	stopC := make(chan struct{})

	epsInformer, err := initInformers(mustKubernetesClient(), resyncPeriod, stopC)
	if err != nil {
		panic(err)
	}

	kcluster := newInMemoryState()
	if err = kcluster.initState(epsInformer); err != nil {
		panic(err)
	}

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)
	<-interrupt
}
