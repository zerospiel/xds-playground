package main

import (
	"context"
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	echo "github.com/zerospiel/xds-playground/pkg/echo_v1"
	echo_messages "github.com/zerospiel/xds-playground/pkg/echo_v1/messages"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
)

var cntServers, defPort int

func init() {
	flag.IntVar(&cntServers, "servers", 1, "how many servers to start")
	flag.IntVar(&defPort, "port", 50051, "default port to start listen server from")
}

type server struct {
	echo.UnimplementedEchoServiceServer
}

func (s server) Echo(_ context.Context, in *echo_messages.EchoRequest) (*echo_messages.EchoResponse, error) {
	log.Printf("---> got RPC call with msg: %s\n", in.GetMsg())
	return &echo_messages.EchoResponse{
		Msg: "echo: " + in.GetMsg(),
	}, nil
}

type healthServer struct{}

func (s healthServer) Check(ctx context.Context, in *healthpb.HealthCheckRequest) (*healthpb.HealthCheckResponse, error) {
	return &healthpb.HealthCheckResponse{Status: healthpb.HealthCheckResponse_SERVING}, nil
}
func (s healthServer) Watch(in *healthpb.HealthCheckRequest, srv healthpb.Health_WatchServer) error {
	return status.Error(codes.Unimplemented, "Watch is not implemented")
}

func main() {
	flag.Parse()

	if len(flag.Args()) > 2 {
		flag.Usage()
		os.Exit(128)
	}

	if cntServers < 1 {
		cntServers = 1
	}
	if cntServers > 10 {
		cntServers = 10
	}

	var servers []*grpc.Server

	wg := sync.WaitGroup{}
	for i := 0; i < cntServers; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()

			port := ":" + strconv.Itoa(defPort+i)
			lis, err := net.Listen("tcp", port)
			if err != nil {
				log.Fatalf("failed listen port %s: %s\n", port, err.Error())
			}

			s := grpc.NewServer()
			echo.RegisterEchoServiceServer(s, &server{})
			healthpb.RegisterHealthServer(s, &healthServer{})
			reflection.Register(s)

			servers = append(servers, s)

			log.Printf("started serving on port %s ...\n", port)
			if err = s.Serve(lis); err != nil {
				log.Fatalf("failed to serve on port %s: %s\n", port, err.Error())
			}
		}()
	}

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)
	st := <-interrupt
	log.Println("got signal", st.String())

	time.Sleep(time.Millisecond * 200)
	shutdown(servers...)
	time.Sleep(time.Millisecond * 200)

	wg.Wait()
}

func shutdown(servers ...*grpc.Server) {
	if len(servers) == 0 {
		return
	}

	wg := sync.WaitGroup{}
	for _, s := range servers {
		s := s
		if s == nil {
			continue
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			done := make(chan struct{})
			go func() {
				s.GracefulStop()
				close(done)
			}()

			select {
			case <-done:
				log.Println("gracefully shutdown server ...")
			case <-time.After(time.Second):
				log.Println("timeout for gracefull shutdown, stoping ...")
				s.Stop()
			}
		}()
	}
	wg.Wait()
}
