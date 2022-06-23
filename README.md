# xdsproxy
xDS proxy server with a internal cache, used for scaling out xDS control planes such as Istio

To build:
go build -o ~/tmp ./...

Run agent in client only mode:
~/tmp/xdsproxy-agent -c -u 127.0.0.1:15020 # useful for debugging

Run agent in proxy mode:
~/tmp/xdsproxy-agent # assume server running at 127.0.0.1:15010
