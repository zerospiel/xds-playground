.PHONY: pb
pb:
	buf protoc --proto_path ./api/ --go_out=Mecho/v1/messages/messages.proto=github.com/zerospiel/xds-playground/pkg/echo_v1/messages:. api/echo/v1/messages/messages.proto api/echo/v1/echo.proto
	buf protoc --proto_path ./api/ --go-grpc_out=Mecho/v1/messages/messages.proto=github.com/zerospiel/xds-playground/pkg/echo_v1/messages:. api/echo/v1/messages/messages.proto api/echo/v1/echo.proto

.PHONY: client
client:
	go run $(CURDIR)/cmd/client --host localhost:50051
	go run $(CURDIR)/cmd/client --host localhost:50052

.PHONY: server
server:
	go run $(CURDIR)/cmd/server --servers 2
