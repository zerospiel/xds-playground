.DEFAULT_GOAL := all

# all is first in case of fallback
# if make binary has version < 3.81
.PHONY: all
all:
	make server &
	make xds &
	make client

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

.PHONY: xds
xds:
	go run $(CURDIR)/cmd/xds --upstream_port 50051 --upstream_port 50052

.PHONY: client_xds
client_xds:
	GRPC_XDS_BOOTSTRAP=$(CURDIR)/cmd/client/xds_bootstrap.json go run $(CURDIR)/cmd/client --host xds:///warden.platform --conns 10
