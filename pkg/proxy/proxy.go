package proxy

import (
	"xdsproxy/pkg/client"
)

type XdsProxy struct {
	adsAsyncClient *client.AsyncClient
}

func NewController(config *ProxyConfig) *XdsProxy {
	proxy := &XdsProxy{}
	return proxy
}
