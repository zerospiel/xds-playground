.PHONY: pb
pb:
	buf protoc --proto_path ./api/ --go_out=Mecho/v1/messages/messages.proto=github.com/zerospiel/xds-playground/pkg/echo_v1/messages,module=github.com/zerospiel/xds-playground:. api/echo/v1/messages/messages.proto api/echo/v1/echo.proto
	buf protoc --proto_path ./api/ --go-grpc_out=Mecho/v1/messages/messages.proto=github.com/zerospiel/xds-playground/pkg/echo_v1/messages,module=github.com/zerospiel/xds-playground:. api/echo/v1/messages/messages.proto api/echo/v1/echo.proto

### localhost related targets

.PHONY: local_client
local_client:
	go run $(CURDIR)/cmd/client --host localhost:50051
	go run $(CURDIR)/cmd/client --host localhost:50052

.PHONY: local_server
local_server:
	go run $(CURDIR)/cmd/server --servers 2

.PHONY: local_xds_server
local_xds_server:
	go run $(CURDIR)/cmd/xds --upstream_port 50051 --upstream_port 50052

.PHONY: local_client_xds
local_client_xds:
	GRPC_XDS_BOOTSTRAP=$(CURDIR)/cmd/client/xds_bootstrap.json go run $(CURDIR)/cmd/client --host xds:///warden.platform --reqs 10

.PHONY: local_client_xds_debug
local_client_xds_debug:
	GRPC_GO_LOG_VERBOSITY_LEVEL=99 GRPC_GO_LOG_SEVERITY_LEVEL=INFO GRPC_XDS_BOOTSTRAP=$(CURDIR)/cmd/client/xds_bootstrap.json go run $(CURDIR)/cmd/client --host xds:///warden.platform --reqs 10

### localhost related targets

### kubernetes related targets

.PHONY: .common_deploy
.common_deploy:
	GOOS=linux go build -o $(CURDIR)/cmd/$(dir)/$(deploy) $(CURDIR)/cmd/$(dir)
	eval $$(minikube docker-env)
	docker build -t $(deploy):latest $(CURDIR)/cmd/$(dir)
	helm upgrade \
		--install $(deploy) \
		--atomic --debug --reset-values \
		--timeout 30s \
		--kube-context minikube \
		--namespace default \
		-f $(CURDIR)/deploy/playground/values_$(deploy).yaml $(CURDIR)/deploy/playground/


.PHONY: xds-server
xds-server:
	@$(MAKE) .common_deploy deploy=$@ dir=xds_k8s

.PHONY: backend
backend:
	@$(MAKE) .common_deploy deploy=$@ dir=server

.PHONY: frontend
frontend:
	@$(MAKE) .common_deploy deploy=$@ dir=client

.PHONY: deploy
deploy: backend xds-server

.PHONY: undeploy
undeploy:
	helm uninstall backend
	helm uninstall xds-server

### kubernetes related targets