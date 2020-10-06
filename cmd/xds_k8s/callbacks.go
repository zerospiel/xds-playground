package main

import (
	"context"
	"log"
	"sync"

	api "github.com/envoyproxy/go-control-plane/envoy/api/v2"
)

type callbacks struct {
	signal   chan struct{}
	fetches  int
	requests int
	mu       sync.Mutex
}

func (cb *callbacks) Report() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	log.Printf("cb.Repost(): callbacks: fetches %d; requests %d\n", cb.fetches, cb.requests)
}

func (cb *callbacks) OnStreamOpen(ctx context.Context, id int64, typ string) error {
	log.Printf("OnStreamOpen %d open for Type [%s]\n", id, typ)
	return nil
}

func (cb *callbacks) OnStreamClosed(id int64) {
	log.Printf("OnStreamClosed %d closed\n", id)
}

func (cb *callbacks) OnStreamRequest(id int64, r *api.DiscoveryRequest) error {
	log.Printf("OnStreamRequest %d  Request[%v]\n", id, r.TypeUrl)
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.requests++
	if cb.signal != nil {
		close(cb.signal)
		cb.signal = nil
	}
	return nil
}

func (cb *callbacks) OnStreamResponse(id int64, req *api.DiscoveryRequest, resp *api.DiscoveryResponse) {
	log.Printf("OnStreamResponse... %d   Request [%v],  Response[%v]\n", id, req.TypeUrl, resp.TypeUrl)
	cb.Report()
}

func (cb *callbacks) OnFetchRequest(ctx context.Context, req *api.DiscoveryRequest) error {
	log.Printf("OnFetchRequest... Request [%v]\n", req.TypeUrl)
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.fetches++
	if cb.signal != nil {
		close(cb.signal)
		cb.signal = nil
	}
	return nil
}

func (cb *callbacks) OnFetchResponse(req *api.DiscoveryRequest, resp *api.DiscoveryResponse) {
	log.Printf("OnFetchResponse... Resquest[%v],  Response[%v]\n", req.TypeUrl, resp.TypeUrl)
}
