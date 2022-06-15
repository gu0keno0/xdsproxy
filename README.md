# xdsproxy
xDS proxy server with a internal cache, used for scaling out xDS control planes such as Istio

To prepare for build:
go get github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3@v0.10.0
go get google.golang.org/grpc@v1.36.0

To build:
go build ./...
