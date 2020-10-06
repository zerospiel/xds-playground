package main

import (
	"flag"
	"log"
	"os"
	"time"
)

var (
	mgmtPort     int
	resyncPeriod time.Duration

	snapshotVersion uint32
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
}
