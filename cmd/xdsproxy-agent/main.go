package main

import (
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	"xdsproxy/pkg/client"
	"xdsproxy/pkg/proxy"
)

var (
	clientOnly   = flag.Bool("c", false, "if set to true, then only run as xDS client for debugging purposes.")
	upstreamAddr = flag.String("u", "127.0.0.1:15010", "upstream xDS server's address.")
	initialListeners = flag.String("t", "*", "initial list of listeners to subscribe to, separated by commas.")
)

func main() {
	flag.Parse()

	// TODO(gu0keno0): add cmdline flags.
	serverConfig := &proxy.ServerConfig{
		GrpcListenAddress:        "127.0.0.1:15020",
		GrpcKeepaliveTime:        30 * time.Second,
		GrpcKeepaliveTimeout:     5 * time.Second,
		GrpcKeepaliveMinTime:     30 * time.Second,
		GrpcMaxConcurrentStreams: 1000000,
	}
	if *clientOnly {
		serverConfig = nil
	}

	proxy := proxy.NewXdsProxy(
		&proxy.ProxyConfig{
			XdsServerConfig: serverConfig,
			AdsClientConfig: &client.AdsClientConfig{
				ServerAddress:          *upstreamAddr,
				MaxPendingRequests:     1000,
				RecvErrBackoffInterval: time.Second,
				XdsNodeInfo: client.NodeInfo{
					ServiceCluster: "svc.cluster.local",
					ServiceNode:    fmt.Sprintf("%s~%s~%s.%s~%s.%s", "sidecar", "127.0.0.1", "", "default", "default", "svc.cluster.local"),
				},
			},
			InitialListeners: strings.Split(*initialListeners, ","),
		},
	)
	log.Println("Proxy is running ...")
	proxy.Run()

	if *clientOnly {
		shutdownChan := make(chan struct{})
		<-shutdownChan
	}
	// TODO(gu0keno0): handle shutdown.
}
