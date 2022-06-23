package proxy

import (
	"time"

	"xdsproxy/pkg/client"
)

type ServerConfig struct {
	GrpcListenAddress        string
	GrpcKeepaliveTime        time.Duration
	GrpcKeepaliveTimeout     time.Duration
	GrpcKeepaliveMinTime     time.Duration
	GrpcMaxConcurrentStreams uint32
}

// ProxyConfig contains both xDS client and xDS server configs
// for xDS proxy.
type ProxyConfig struct {
	XdsServerConfig *ServerConfig
	AdsClientConfig *client.AdsClientConfig
}
