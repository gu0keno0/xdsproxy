package client

import (
	"time"

	envoy_config_core_v3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	_struct "github.com/golang/protobuf/ptypes/struct"
)

// NodeInfo identifies an xDS client node.
type NodeInfo struct {
	ServiceCluster string
	ServiceNode    string
	Metadata       *_struct.Struct
}

// AdsClientConfig contains configuration options for an ADS client,
// it specifies how ADS client should manage connections and send requests
// to ADS servers.
type AdsClientConfig struct {
	// ServerAddress is the gRPC address of ADS server.
	ServerAddress string
	// MaxPendingRequests is the max number of pending requests on an
	// async ADS client.
	MaxPendingRequests uint32
	// RecvErrBackoffInterval is the retry backoff interval on receiving errors.
	RecvErrBackoffInterval time.Duration

	// TODO(gu0keno0): configs for gRPC bidi-streaming flow control.

	XdsNodeInfo NodeInfo
}

func (adscc *AdsClientConfig) Node() *envoy_config_core_v3.Node {
	return &envoy_config_core_v3.Node{
		Id:       adscc.XdsNodeInfo.ServiceNode,
		Cluster:  adscc.XdsNodeInfo.ServiceCluster,
		Metadata: adscc.XdsNodeInfo.Metadata,
	}
}
