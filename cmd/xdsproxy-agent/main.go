package main

import (
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	"xdsproxy/pkg/client"
	"xdsproxy/pkg/proxy"

	structpb "github.com/golang/protobuf/ptypes/struct"
)

var (
	clientOnly   = flag.Bool("c", false, "if set to true, then only run as xDS client for debugging purposes.")
	upstreamAddr = flag.String("u", "127.0.0.1:15010", "upstream xDS server's address.")
	initialListeners = flag.String("t", "*", "initial list of listeners to subscribe to, separated by commas.")
	useGrpcGenerator = flag.Bool("g", false, "if set, then use gRPC xDS generator")
	concurrency = flag.Int("p", 1, "number of concurrent clients, for benchmarking xDS control plane")
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


	metadata := &structpb.Struct{
		Fields: map[string]*structpb.Value{},
	}
	if *useGrpcGenerator {
		metadata.Fields["GENERATOR"] = &structpb.Value {
			Kind: &structpb.Value_StringValue{
				StringValue: "grpc",
			},
		}
	}

	for i := 0; i < *concurrency; i++ {
		proxy := proxy.NewXdsProxy(
			&proxy.ProxyConfig{
				XdsServerConfig: serverConfig,
				AdsClientConfig: &client.AdsClientConfig{
					ServerAddress:          *upstreamAddr,
					MaxPendingRequests:     1000,
					RecvErrBackoffInterval: time.Second,
					XdsNodeInfo: client.NodeInfo{
						ServiceCluster: "svc.cluster.local",
						ServiceNode:    fmt.Sprintf("%s~%s~%s.%s~%s.%s", "sidecar", "127.0.0.1", "default", fmt.Sprintf("%v", i), "default", "svc.cluster.local"),
						Metadata: metadata,
					},
				},
				InitialListeners: strings.Split(*initialListeners, ","),
			},
		)
		log.Printf("Proxy %v is running ...\n", i)
		go proxy.Run()
	}

	shutdownChan := make(chan struct{})
	<-shutdownChan
	// TODO(gu0keno0): handle shutdown.
}
