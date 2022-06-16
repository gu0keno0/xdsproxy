package main

import (
	"fmt"
	"log"
	"time"

	"xdsproxy/pkg/client"
	"xdsproxy/pkg/proxy"
)

func main() {
	// TODO(gu0keno0): add cmdline flags.
	proxy := proxy.NewXdsProxy(
		&proxy.ProxyConfig{
			AdsClientConfig: &client.AdsClientConfig{
				ServerAddress:          "127.0.0.1:15010",
				MaxPendingRequests:     1000,
				RecvErrBackoffInterval: time.Second,
				XdsNodeInfo: client.NodeInfo{
					ServiceCluster: "svc.cluster.local",
					ServiceNode:    fmt.Sprintf("%s~%s~%s.%s~%s.%s", "sidecar", "127.0.0.1", "", "default", "default", "svc.cluster.local"),
				},
			},
		},
	)
	proxy.Run()

	// TODO(gu0keno0): handle shutdown.
	log.Println("Proxy is running ...")
	shutdownChan := make(chan struct{})
	<-shutdownChan
}
