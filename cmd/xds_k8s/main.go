package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	api "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	xds "github.com/envoyproxy/go-control-plane/pkg/server/v2"
	"google.golang.org/grpc"
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

	ctx, cancel := context.WithCancel(context.Background())
	xdsServer := xds.NewServer(ctx, kcluster.cacheAny, &callbacks{
		signal:   make(chan struct{}),
		fetches:  0,
		requests: 0,
	})

	go runMgmtServer(ctx, xdsServer, mgmtPort)

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL)
	select {
	case <-ctx.Done():
	case <-interrupt:
	}
	cancel() // shutdown server
}

func runMgmtServer(ctx context.Context, xdsServer xds.Server, port int) {
	grpcServer := grpc.NewServer(grpc.MaxConcurrentStreams(1000))
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Fatalf("failed to listen to :%d: %s", port, err.Error())
	}

	discovery.RegisterAggregatedDiscoveryServiceServer(grpcServer, xdsServer)
	api.RegisterEndpointDiscoveryServiceServer(grpcServer, xdsServer)
	api.RegisterClusterDiscoveryServiceServer(grpcServer, xdsServer)
	api.RegisterRouteDiscoveryServiceServer(grpcServer, xdsServer)
	api.RegisterListenerDiscoveryServiceServer(grpcServer, xdsServer)

	if err = grpcServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve grpc server on :%d: %s", port, err.Error())
	}

	<-ctx.Done()
	shutdown(grpcServer)
}

func shutdown(server *grpc.Server) {
	if server == nil {
		return
	}
	done := make(chan struct{})
	go func() {
		server.GracefulStop()
		close(done)
	}()

	select {
	case <-done:
		log.Println("gracefully shutdown server ...")
	case <-time.After(time.Second):
		log.Println("timeout for gracefull shutdown, stoping ...")
		server.Stop()
	}
}
