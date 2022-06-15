package proxy

import (
	"xdsproxy/pkg/client"
)

// ProxyConfig contains both xDS client and xDS server configs
// for xDS proxy.
type ProxyConfig struct {
	AdsClientConfig *client.AdsClientConfig
}
