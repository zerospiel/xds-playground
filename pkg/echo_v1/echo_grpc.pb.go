// Code generated by protoc-gen-go-grpc. DO NOT EDIT.

package echo

import (
	context "context"
	messages "github.com/zerospiel/xds-playground/pkg/echo_v1/messages"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
const _ = grpc.SupportPackageIsVersion6

// EchoServiceClient is the client API for EchoService service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
type EchoServiceClient interface {
	Echo(ctx context.Context, in *messages.EchoRequest, opts ...grpc.CallOption) (*messages.EchoResponse, error)
}

type echoServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewEchoServiceClient(cc grpc.ClientConnInterface) EchoServiceClient {
	return &echoServiceClient{cc}
}

func (c *echoServiceClient) Echo(ctx context.Context, in *messages.EchoRequest, opts ...grpc.CallOption) (*messages.EchoResponse, error) {
	out := new(messages.EchoResponse)
	err := c.cc.Invoke(ctx, "/echo.v1.EchoService/Echo", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// EchoServiceServer is the server API for EchoService service.
// All implementations must embed UnimplementedEchoServiceServer
// for forward compatibility
type EchoServiceServer interface {
	Echo(context.Context, *messages.EchoRequest) (*messages.EchoResponse, error)
	mustEmbedUnimplementedEchoServiceServer()
}

// UnimplementedEchoServiceServer must be embedded to have forward compatible implementations.
type UnimplementedEchoServiceServer struct {
}

func (*UnimplementedEchoServiceServer) Echo(context.Context, *messages.EchoRequest) (*messages.EchoResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Echo not implemented")
}
func (*UnimplementedEchoServiceServer) mustEmbedUnimplementedEchoServiceServer() {}

func RegisterEchoServiceServer(s *grpc.Server, srv EchoServiceServer) {
	s.RegisterService(&_EchoService_serviceDesc, srv)
}

func _EchoService_Echo_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(messages.EchoRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(EchoServiceServer).Echo(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/echo.v1.EchoService/Echo",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(EchoServiceServer).Echo(ctx, req.(*messages.EchoRequest))
	}
	return interceptor(ctx, in, info, handler)
}

var _EchoService_serviceDesc = grpc.ServiceDesc{
	ServiceName: "echo.v1.EchoService",
	HandlerType: (*EchoServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "Echo",
			Handler:    _EchoService_Echo_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "echo/v1/echo.proto",
}
