# xdsproxy
xDS proxy server with a internal cache, used for scaling out xDS control planes such as Istio

To build:
go get -d -v ./... && go build -o ~/tmp ./...

Run agent in client only mode:
~/tmp/xdsproxy-agent -c -u 127.0.0.1:15020 # useful for debugging

Run agent in proxy mode:
~/tmp/xdsproxy-agent # assume server running at 127.0.0.1:15010

Set initial LDS resource name:
echo > /tmp/1 ; ~/tmp/xdsproxy-agent -c -u 127.0.0.1:15010 -t "d2-service-envoy-benchmark.prod.linkedin.com_18080" 2>&1 | tee /tmp/1
echo > /tmp/1 ; ~/tmp/xdsproxy-agent -c -u 127.0.0.1:15010 -t "d2-service-polarisfoo1.prod.linkedin.com_18080,d2-service-envoy-benchmark.prod.linkedin.com_18080" 2>&1 | tee /tmp/1

Benchmark Control Plane by specifying concurrency via -p:
~/tmp/xdsproxy-agent -c -u 127.0.0.1:15010 -t "indis-service-2300.prod.linkedin.com:18080" -g -p 1000
