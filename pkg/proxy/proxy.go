package proxy

import (
	"fmt"
	"log"

	envoy_config_listener_v3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	envoy_extensions_filters_network_http_connection_manager_v3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	envoy_service_discovery_v3 "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	xds_resource "github.com/envoyproxy/go-control-plane/pkg/resource/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"github.com/golang/protobuf/ptypes"
	"xdsproxy/pkg/client"
)

const (
	GLOBAL_LDS_CONFIG_NAME = "0.0.0.0_18080"
	GLOBAL_LISTNER_NAME    = "0.0.0.0_18080"
)

// XdsProxy implements a xDS proxy for upstream Istio cluster with caching
// capability. The xDS requests to the proxy will be served by cache first and
// on cache misses, the proxy will request the resource from the upstream
// Istio cluster.
type XdsProxy struct {
	config         *ProxyConfig
	adsAsyncClient *client.AsyncClient
}

func NewXdsProxy(config *ProxyConfig) *XdsProxy {
	proxy := &XdsProxy{
		config: config,
	}
	proxy.adsAsyncClient = client.NewAsyncClient(config.AdsClientConfig, proxy)

	return proxy
}

func (proxy *XdsProxy) Run() {
	// Run the upstream client req / resp loops, XdsProxy.HandleAds will be
	// invoked as responses are streamed back from upstream xDS server, and
	// we populate local xDS cache there.
	proxy.adsAsyncClient.Run()
}

func (proxy *XdsProxy) GetInitialAdsRequest() *envoy_service_discovery_v3.DeltaDiscoveryRequest {
	// TODO(gu0keno0): implement initial version check.
	// Start with wildcard LDS and CDS requests.
	req := &envoy_service_discovery_v3.DeltaDiscoveryRequest{
		Node:                   proxy.config.AdsClientConfig.Node(),
		TypeUrl:                xds_resource.ListenerType,
		ResourceNamesSubscribe: []string{GLOBAL_LDS_CONFIG_NAME},
	}
	return req
}

func (proxy *XdsProxy) handleLds(resp *envoy_service_discovery_v3.DeltaDiscoveryResponse) error {
	for _, res := range resp.Resources {
		listener := &envoy_config_listener_v3.Listener{}
		if err := ptypes.UnmarshalAny(res.Resource, listener); err != nil {
			log.Printf("ADSClient unmarshal listener fail: %v\n", err)
			return err
		}

		// TODO(gu0keno0): populate LDS cache. Version is in Resource object.
		if listener.Name == GLOBAL_LISTNER_NAME {
			log.Printf("Got global listener: %v", listener)
			rds_config_names := []string{}
			for _, chain := range listener.FilterChains {
				for _, filter := range chain.Filters {
					if filter.Name != wellknown.HTTPConnectionManager {
						continue
					}
					config := xds_resource.GetHTTPConnectionManager(filter)
					if config == nil {
						continue
					}
					rds, ok := config.RouteSpecifier.(*envoy_extensions_filters_network_http_connection_manager_v3.HttpConnectionManager_Rds)
					if ok && rds != nil && rds.Rds != nil {
						rds_config_names = append(rds_config_names, rds.Rds.RouteConfigName)
					}
				}
			}
			log.Printf("Got RDS config names: %v from %v", rds_config_names, GLOBAL_LISTNER_NAME)
			// Send RDS request.
			req := &envoy_service_discovery_v3.DeltaDiscoveryRequest{
				Node:                   proxy.config.AdsClientConfig.Node(),
				TypeUrl:                xds_resource.RouteType,
				ResourceNamesSubscribe: rds_config_names,
			}
			// TODO(gu0keno0): handle ratelimit errors.
			proxy.adsAsyncClient.Send(req)
		}
	}
	return nil
}

func (proxy *XdsProxy) HandleAds(client *client.AsyncClient, resp *envoy_service_discovery_v3.DeltaDiscoveryResponse) error {
	log.Printf("Got ADS response: %v\n", resp)
	if resp.TypeUrl == xds_resource.ListenerType {
		return proxy.handleLds(resp)
	}
	// TODO(gu0keno0): implement xDS cache population.
	return fmt.Errorf("Unhandled Ads response: %v", resp)
}
