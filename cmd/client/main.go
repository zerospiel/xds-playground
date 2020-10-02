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
	host    string
	connCnt int
)

func init() {
	flag.StringVar(&host, "host", "", "host to send request to in form of [sheme://][authority/]hostname[:port]")
	flag.IntVar(&connCnt, "conns", 3, "how many connections (actually clients) to create")
}

func main() {
	flag.Parse()
	if len(host) == 0 {
		flag.Usage()
		os.Exit(128)
	}

	for i := 0; i < connCnt; i++ {
		conn, err := grpc.Dial(host, grpc.WithInsecure())
		if err != nil {
			log.Fatalf("failed dial '%s': %s\n", host, err.Error())
		}
		defer conn.Close()

		cl := echo.NewEchoServiceClient(conn)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		p := &peer.Peer{}
		resp, err := cl.Echo(ctx, &echo_messages.EchoRequest{
			Msg: "hello " + strconv.Itoa(i),
		}, grpc.Peer(p))
		if err != nil {
			log.Fatalf("failed invoke Echo: %s\n", err.Error())
		}
		log.Printf("RPC response from '%s': %s", p.Addr.String(), resp.GetMsg())
		time.Sleep(time.Millisecond * 500)
	}
}
