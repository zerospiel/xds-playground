package main

import (
	"context"
	"flag"
	"log"
	"os"
	"strconv"
	"time"

	echo "github.com/zerospiel/xds-playground/pkg/echo_v1"
	echo_messages "github.com/zerospiel/xds-playground/pkg/echo_v1/messages"
	"google.golang.org/grpc"
	"google.golang.org/grpc/peer"
	_ "google.golang.org/grpc/xds"
)

var (
	host         string
	reqCnt       int
	restDuration time.Duration
)

func init() {
	flag.StringVar(&host, "host", "", "host to send request to in form of [sheme://][authority/]hostname[:port]")
	flag.IntVar(&reqCnt, "reqs", 3, "how many requests send to server, -1 for endless")
	flag.DurationVar(&restDuration, "restdur", time.Second, "hot many time to sleep btw each request")
}

func main() {
	flag.Parse()
	if len(host) == 0 {
		flag.Usage()
		os.Exit(128)
	}

	conn, err := grpc.Dial(host, grpc.WithInsecure())
	if err != nil {
		log.Fatalf("failed dial '%s': %s\n", host, err.Error())
	}
	defer conn.Close()

	cl := echo.NewEchoServiceClient(conn)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var i int
	for {
		p := &peer.Peer{}
		resp, err := cl.Echo(ctx, &echo_messages.EchoRequest{
			Msg: "hello " + strconv.Itoa(i),
		}, grpc.Peer(p))
		if err != nil {
			log.Fatalf("failed invoke Echo: %s\n", err.Error())
		}
		log.Printf("RPC response from '%s': %s (%s)", p.Addr.String(), resp.GetMsg(), mustHostname())
		time.Sleep(restDuration)

		if reqCnt != -1 {
			i++
			if i >= reqCnt {
				break
			}
		}
	}
}

func mustHostname() string {
	h, _ := os.Hostname()
	return h
}
