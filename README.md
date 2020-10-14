# XDS PLAYGROUND

## Description

This is an example of XDS server implementation using [envoy-control-plane](https://github.com/envoyproxy/go-control-plane) and [grpc-go XDS package](https://github.com/grpc/grpc-go/tree/master/xds).

There are some gRPC servers (backends), some XDS management servers (depends on local or k8s versions), and some clients (frontends) that make several RPC calls to the backend. 

All required discovery services (resources) prepared by the XDS management server using `envoy`'s control plane, and every request from the client being resolved by `grpc-go` `xds` resolver. Under the hood, there is some magic that `grpc-go` doing with its `service config`. Check out [proposal][proposal] for more information about the basic principles of xDS requests and response processing.

The localhost example supports simple and naive emulation of updating/adding new endpoints (just RR through upstreams) to represent balancing within 2 backends.

The k8s example is a more robust way to represent how it will work in your k8s cluster. 

>Note that for simplifications the k8s example produces snapshots of the whole cluster state (in other words — `State of the World`), **do not use it on production** since this is a big overhead because every frontend will receive the whole cache every time.

I mostly didn't care about system design of this playground repository, so there is a bunch of boilerplate code. This playground was a kind of research for me, do not shame on me 🤗

## Usage

### Localhost example

Run the following commands in the root path of the project each in a separate terminal for readability:

```shell
$ make local_server
$ make local_xds_server
$ make local_client_xds # run target local_client_xds_debug to enable grpc verbose output
```

### k8s example

I use minikube as a local k8s cluster, so you should install it (i.e. on macOS using [brew](https://brew.sh/)):

```shell
$ brew install minikube
```

Use the following targets to simply run the whole example, then check out logs to investigate what's going on:

```shell
$ minikube start # start k8s cluster
$ eval $(minikube docker-env) # escale using local registry, works only on terminal where you entered this command
$ make deploy # deploy services
```

To undeploy services simply run `make undeploy`.

To check out logs run the following commands:

```shell
$ kubectl logs -lapp=xds-server # xds-server
$ kubectl logs -lapp=backend # backend
$ kubectl logs -lapp=frontend # frontend with grpc debug level
```

## Further steps

The main idea of these examples is to show a quick presentation of how to bake `grpc-go` with its `xds` resolver ([v1.32.0](https://github.com/grpc/grpc-go/releases/tag/v1.32.0) at this moment).

The main problem is the usage of [snapshots](https://pkg.go.dev/github.com/envoyproxy/go-control-plane@v0.9.7/pkg/cache/v2#Snapshot), this is not very flexible because it binds us to customize work with custom `nodeIDs` using custom [callbacks](https://pkg.go.dev/github.com/envoyproxy/go-control-plane@v0.9.7/pkg/server/v2#CallbackFuncs) or use SotW snapshots (that is a big overhead).

Potentially this could be resolved in a simple way — using your own cache (i.e. [LinearCache](https://pkg.go.dev/github.com/envoyproxy/go-control-plane@v0.9.7/pkg/cache/v2#LinearCache)) and updating resources by hand simply calling for [UpdateResouce](https://pkg.go.dev/github.com/envoyproxy/go-control-plane@v0.9.7/pkg/cache/v2#LinearCache.UpdateResource) method of cache instance.

## References

1. [xDS-Based Global Load Balancing Proposal][proposal]
1. [Envoy's example of usage and configuring control plane][dyplomat]
1. [This article with pretty good example of usage and control plane configuring][article]

[proposal]:https://github.com/grpc/proposal/blob/master/A27-xds-global-load-balancing.md

[dyplomat]:https://github.com/envoyproxy/go-control-plane/blob/master/examples/dyplomat/readme.md

[article]:https://medium.com/@salmaan.rashid/grpc-xds-loadbalancing-a05f8bd754b8
