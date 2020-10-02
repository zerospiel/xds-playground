# XDS PLAYGROUND

## Description

This is an example of XDS server implementation using [envoy-control-plane](https://github.com/envoyproxy/go-control-plane) and [grpc-go XDS package](https://github.com/grpc/grpc-go/tree/master/xds).

There are 2 gRPC servers (backends), single XDS management server and a single client that makes several RPC calls to some XDS server (`warden.platform` in this example). All required discovery services are prepared by `envoy`'s control plane, and every request from client being resolved by `grpc-go` `xds` resolver. Under the hood there is some magic that `grpc-go` doing with its `service config`.

The example currently supports simple and naive emulation of updating / adding new endpoints (just RR through upstreams) to represent balancing within 2 backends.

## Usage

> Currently this example supports localhost only startup

Run the following commands in the root path of the project each in separate terminal for readability:

```shell
$ make server
$ make xds
$ make xds_client # run target xds_client_debug to enable grpc verbose output
```

## References

1. [xDS-Based Global Load Balancing Proposal](https://github.com/grpc/proposal/blob/master/A27-xds-global-load-balancing.md)
1. [This article with pretty good example of usage and control plane configuring](https://medium.com/@salmaan.rashid/grpc-xds-loadbalancing-a05f8bd754b8)
1. [Envoy's example of usage and configuring control plane](https://github.com/envoyproxy/go-control-plane/blob/master/examples/dyplomat/readme.md)