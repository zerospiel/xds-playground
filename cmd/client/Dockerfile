FROM golang:latest

COPY frontend /bin/frontend-linux

ENV GRPC_GO_LOG_VERBOSITY_LEVEL=99
ENV GRPC_GO_LOG_SEVERITY_LEVEL=info
ENV GRPC_XDS_BOOTSTRAP=/var/run/config/xds_bootstrap.json

ENTRYPOINT [ "/bin/frontend-linux" ]
