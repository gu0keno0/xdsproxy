package proxy

import (
	"errors"
	"fmt"
	"log"
	"strings"

	envoy_config_cluster_v3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	envoy_config_endpoint_v3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	envoy_config_listener_v3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	envoy_config_route_v3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	envoy_extensions_filters_network_http_connection_manager_v3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	envoy_service_discovery_v3 "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	xds_resource "github.com/envoyproxy/go-control-plane/pkg/resource/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"github.com/golang/protobuf/ptypes"
	"xdsproxy/pkg/client"
)

const (
	GLOBAL_LDS_CONFIG_NAME   = "0.0.0.0_18080"
	GLOBAL_LISTNER_NAME      = "0.0.0.0_18080"
	GLOBAL_VIRTUAL_HOST_NAME_SUFFIX = ":18080"
	CLUSTER_NAME_SUFFIX = ".com"
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

func (proxy *XdsProxy) handleRds(resp *envoy_service_discovery_v3.DeltaDiscoveryResponse) error {
	for _, res := range resp.Resources {
		route_config := &envoy_config_route_v3.RouteConfiguration{}
		if err := ptypes.UnmarshalAny(res.Resource, route_config); err != nil {
			log.Printf("ADSClient unmarshal route_config fail: %v\n", err)
			return err
		}
		log.Printf("Got route_config: %v", route_config)
		for _, vh := range route_config.VirtualHosts {
			if !strings.HasSuffix(vh.Name, GLOBAL_VIRTUAL_HOST_NAME_SUFFIX) {
				continue
			}
			clusters := make(map[string]bool)
			log.Printf("Got global virtual host: %v", vh)
			// TODO(gu0keno0): cache RDS
			for _, route := range vh.Routes {
				log.Printf("Got route %v (under %v): %v", route.Name, vh.Name, route)
				route_action_wrapper, ok := route.Action.(*envoy_config_route_v3.Route_Route)
				if !ok {
					return fmt.Errorf("unsupported route action: %v", route.Action)
				}
				cluster, ok := route_action_wrapper.Route.ClusterSpecifier.(*envoy_config_route_v3.RouteAction_Cluster)
				if !ok {
					return fmt.Errorf("no cluster target found under route action: %v", route.Action)
				}
				clusters[cluster.Cluster] = true
			}
			log.Printf("Got clusters %v, sending CDS", clusters)
			cluster_names := []string{}
			for cn, _ := range clusters {
				cluster_names = append(cluster_names, cn)
			}
			req := &envoy_service_discovery_v3.DeltaDiscoveryRequest{
				Node:                   proxy.config.AdsClientConfig.Node(),
				TypeUrl:                xds_resource.ClusterType,
				ResourceNamesSubscribe: cluster_names,
			}
			proxy.adsAsyncClient.Send(req)
			return nil
		}
	}
	return errors.New("cannot find matching routes")
}

func (proxy *XdsProxy) handleCds(resp *envoy_service_discovery_v3.DeltaDiscoveryResponse) error {
	for _, res := range resp.Resources {
		cluster := &envoy_config_cluster_v3.Cluster{}
		if err := ptypes.UnmarshalAny(res.Resource, cluster); err != nil {
			log.Printf("ADSClient unmarshal cluster fail: %v\n", err)
			return err
		}
		// TODO(gu0keno0): better filtering of cluster names.
		if strings.HasPrefix(cluster.Name, "outbound") && !strings.HasPrefix(cluster.Name, "outbound|18080") && strings.HasSuffix(cluster.Name, CLUSTER_NAME_SUFFIX) {
			log.Printf("Got cluster: %v", cluster)
			// TODO(gu0keno0): cache clusters.
			eds_config_names := []string{}
			if cluster_type, ok := cluster.ClusterDiscoveryType.(*envoy_config_cluster_v3.Cluster_Type); ok {
				if cluster_type.Type == envoy_config_cluster_v3.Cluster_EDS {
					if cluster.EdsClusterConfig != nil && cluster.EdsClusterConfig.ServiceName != "" {
						eds_config_names = append(eds_config_names, cluster.EdsClusterConfig.ServiceName)
					}
				}
			}
			log.Printf("Got EDS configs: %v\n", eds_config_names)
			req := &envoy_service_discovery_v3.DeltaDiscoveryRequest{
				Node:                   proxy.config.AdsClientConfig.Node(),
				TypeUrl:                xds_resource.EndpointType,
				ResourceNamesSubscribe: eds_config_names,
			}
			proxy.adsAsyncClient.Send(req)
		}
	}
	return nil
}

func (proxy *XdsProxy) handleEds(resp *envoy_service_discovery_v3.DeltaDiscoveryResponse) error {
	for _, res := range resp.Resources {
		cluster_load_assignment := &envoy_config_endpoint_v3.ClusterLoadAssignment{}
		if err := ptypes.UnmarshalAny(res.Resource, cluster_load_assignment); err != nil {
			return err
		}
		//TODO(gu0keno0): cache for endpoints.
		log.Printf("Got Cluster Load Assignment: %v\n", cluster_load_assignment)
	}
	return nil
}

func (proxy *XdsProxy) HandleAds(client *client.AsyncClient, resp *envoy_service_discovery_v3.DeltaDiscoveryResponse) error {
	log.Printf("Got ADS response: %v\n", resp)
	if resp.TypeUrl == xds_resource.ListenerType {
		return proxy.handleLds(resp)
	} else if resp.TypeUrl == xds_resource.RouteType {
		return proxy.handleRds(resp)
	} else if resp.TypeUrl == xds_resource.ClusterType {
		return proxy.handleCds(resp)
	} else if resp.TypeUrl == xds_resource.EndpointType {
		return proxy.handleEds(resp)
	}
	// TODO(gu0keno0): implement xDS cache population.
	return fmt.Errorf("Unhandled Ads response: %v", resp)
}
