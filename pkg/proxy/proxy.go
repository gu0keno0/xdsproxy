package proxy

import (
	"context"
	"fmt"
	"log"
	"net"
	"strings"

	envoy_config_cluster_v3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	envoy_config_endpoint_v3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	envoy_config_listener_v3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	envoy_config_route_v3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	envoy_extensions_filters_network_http_connection_manager_v3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	clusterservice "github.com/envoyproxy/go-control-plane/envoy/service/cluster/v3"
	discoverygrpc "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	envoy_service_discovery_v3 "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	endpointservice "github.com/envoyproxy/go-control-plane/envoy/service/endpoint/v3"
	listenerservice "github.com/envoyproxy/go-control-plane/envoy/service/listener/v3"
	routeservice "github.com/envoyproxy/go-control-plane/envoy/service/route/v3"
	runtimeservice "github.com/envoyproxy/go-control-plane/envoy/service/runtime/v3"
	secretservice "github.com/envoyproxy/go-control-plane/envoy/service/secret/v3"
	xds_resource "github.com/envoyproxy/go-control-plane/pkg/resource/v3"
	"github.com/envoyproxy/go-control-plane/pkg/server/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"github.com/golang/protobuf/ptypes"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
	"xdsproxy/pkg/cache"
	"xdsproxy/pkg/client"
)

const (
	GLOBAL_LISTNER_NAME_SUFFIX		= "_18080"
	GLOBAL_VIRTUAL_HOST_NAME_SUFFIX = ":18080"
	CLUSTER_NAME_SUFFIX             = ".com"
)

// XdsProxy implements a xDS proxy for upstream Istio cluster with caching
// capability. The xDS requests to the proxy will be served by cache first and
// on cache misses, the proxy will request the resource from the upstream
// Istio cluster.
type XdsProxy struct {
	config         *ProxyConfig
	adsAsyncClient *client.AsyncClient
	cache          *cache.XdsCache
	proxyServer    server.Server

	cdsInitialized bool
	rdsInitialized bool
	edsInitialized map[string]struct{}
}

func NewXdsProxy(config *ProxyConfig) *XdsProxy {
	proxy := &XdsProxy{
		config: config,
	}
	proxy.adsAsyncClient = client.NewAsyncClient(config.AdsClientConfig, proxy)
	proxy.cache = cache.NewXdsCache()

	callback := server.CallbackFuncs{
		StreamRequestFunc:      proxy.OnStreamRequest,
		StreamDeltaRequestFunc: proxy.OnStreamDeltaRequest,
	}
	proxy.proxyServer = server.NewServer(context.Background(), proxy.cache, callback)

	proxy.edsInitialized = make(map[string]struct{})

	return proxy
}

func (proxy *XdsProxy) runServer() {
	var grpcOptions []grpc.ServerOption
	grpcOptions = append(grpcOptions,
		grpc.MaxConcurrentStreams(proxy.config.XdsServerConfig.GrpcMaxConcurrentStreams),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:    proxy.config.XdsServerConfig.GrpcKeepaliveTime,
			Timeout: proxy.config.XdsServerConfig.GrpcKeepaliveTimeout,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             proxy.config.XdsServerConfig.GrpcKeepaliveMinTime,
			PermitWithoutStream: true,
		}),
	)
	grpcServer := grpc.NewServer(grpcOptions...)

	lis, err := net.Listen("tcp", proxy.config.XdsServerConfig.GrpcListenAddress)
	if err != nil {
		log.Fatal(err)
	}

	discoverygrpc.RegisterAggregatedDiscoveryServiceServer(grpcServer, proxy.proxyServer)
	endpointservice.RegisterEndpointDiscoveryServiceServer(grpcServer, proxy.proxyServer)
	clusterservice.RegisterClusterDiscoveryServiceServer(grpcServer, proxy.proxyServer)
	routeservice.RegisterRouteDiscoveryServiceServer(grpcServer, proxy.proxyServer)
	listenerservice.RegisterListenerDiscoveryServiceServer(grpcServer, proxy.proxyServer)
	secretservice.RegisterSecretDiscoveryServiceServer(grpcServer, proxy.proxyServer)
	runtimeservice.RegisterRuntimeDiscoveryServiceServer(grpcServer, proxy.proxyServer)

	log.Printf("management server listening on %v\n", proxy.config.XdsServerConfig.GrpcListenAddress)
	if err = grpcServer.Serve(lis); err != nil {
		log.Println(err)
	}
}

func (proxy *XdsProxy) Run() {
	// Run the upstream client req / resp loops, XdsProxy.HandleAds will be
	// invoked as responses are streamed back from upstream xDS server, and
	// we populate local xDS cache there.
	proxy.adsAsyncClient.Run()
	// Run xDS server.
	if proxy.config.XdsServerConfig != nil {
		proxy.runServer()
	}
}

func (proxy *XdsProxy) GetInitialAdsRequest() *envoy_service_discovery_v3.DeltaDiscoveryRequest {
	// TODO(gu0keno0): implement initial version check.
	// Start with wildcard LDS and CDS requests.
	req := &envoy_service_discovery_v3.DeltaDiscoveryRequest{
		Node:                   proxy.config.AdsClientConfig.Node(),
		TypeUrl:                xds_resource.ListenerType,
		ResourceNamesSubscribe: proxy.config.InitialListeners,
	}
	/*req := &envoy_service_discovery_v3.DeltaDiscoveryRequest{
	    Node:                   proxy.config.AdsClientConfig.Node(),
	    TypeUrl:                xds_resource.ClusterType,
	    ResourceNamesSubscribe: []string{"*"},
	}*/
	return req
}

func (proxy *XdsProxy) OnStreamRequest(streamID int64, req *discoverygrpc.DiscoveryRequest) error {
	deltaReq := &envoy_service_discovery_v3.DeltaDiscoveryRequest{
		Node:                   proxy.config.AdsClientConfig.Node(),
		TypeUrl:                req.TypeUrl,
		ResourceNamesSubscribe: req.ResourceNames,
	}
	// TODO(gu0keno0): handle ratelimit errors.
	return proxy.adsAsyncClient.Send(deltaReq)
}

func (proxy *XdsProxy) OnStreamDeltaRequest(streamID int64, req *discoverygrpc.DeltaDiscoveryRequest) error {
	// TODO(gu0keno0): use cache to avoid sending duplicated requests.
	// TODO(gu0keno0): handle wildcard requests.
	// TODO(gu0keno0): handle initial request version comparisons.
	deltaReq := &envoy_service_discovery_v3.DeltaDiscoveryRequest{
		Node:                     proxy.config.AdsClientConfig.Node(),
		TypeUrl:                  req.TypeUrl,
		ResourceNamesSubscribe:   req.ResourceNamesSubscribe,
		ResourceNamesUnsubscribe: req.ResourceNamesUnsubscribe,
	}
	// TODO(gu0keno0): handle ratelimit errors.
	return proxy.adsAsyncClient.Send(deltaReq)
}

func (proxy *XdsProxy) handleLds(resp *envoy_service_discovery_v3.DeltaDiscoveryResponse) error {
	rds_config_names := []string{}
	for _, res := range resp.Resources {
		listener := &envoy_config_listener_v3.Listener{}
		if err := ptypes.UnmarshalAny(res.Resource, listener); err != nil {
			log.Printf("ADSClient unmarshal listener fail: %v\n", err)
			return err
		}

		// TODO(gu0keno0): use resource version to avoid duplicated LDS updates.
		proxy.cache.SetResource(xds_resource.ListenerType, listener.Name, listener)

		// TODO(gu0keno0): better handling of initialization, maybe we just start with RDS directly.
		if proxy.rdsInitialized {
			log.Printf("RDS initial request sent already, not going to send further RDS requests for LDS response.")
			continue
		}

		if strings.HasSuffix(listener.Name, GLOBAL_LISTNER_NAME_SUFFIX) {
			log.Printf("Got global listener: %v", listener)
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
			log.Printf("Got RDS config names: %v from %v", rds_config_names, listener.Name)
		}
	}

	// TODO(gu0keno0): better handling RDS initialization, using local file cache to reduce bootstrap overhead.
    if !proxy.rdsInitialized {
        // Send RDS request.
        req := &envoy_service_discovery_v3.DeltaDiscoveryRequest{
            Node:                   proxy.config.AdsClientConfig.Node(),
            TypeUrl:                xds_resource.RouteType,
            ResourceNamesSubscribe: rds_config_names,
        }
        // TODO(gu0keno0): handle ratelimit errors.
        proxy.adsAsyncClient.Send(req)
        proxy.rdsInitialized = true
    } else {
        log.Printf("RDS initial request sent already, not going to send further RDS requests for LDS response.")
    }

	return nil
}

func (proxy *XdsProxy) handleRds(resp *envoy_service_discovery_v3.DeltaDiscoveryResponse) error {
	cluster_names := []string{}
	for _, res := range resp.Resources {
		route_config := &envoy_config_route_v3.RouteConfiguration{}
		if err := ptypes.UnmarshalAny(res.Resource, route_config); err != nil {
			log.Printf("ADSClient unmarshal route_config fail: %v\n", err)
			return err
		}
		log.Printf("Got route_config: %v", route_config)
		// TODO(gu0keno0): use resource version to avoid duplicated RDS updates.
		proxy.cache.SetResource(xds_resource.RouteType, route_config.Name, route_config)
		for _, vh := range route_config.VirtualHosts {
			if !strings.HasSuffix(vh.Name, GLOBAL_VIRTUAL_HOST_NAME_SUFFIX) {
				continue
			}
			clusters := make(map[string]bool)
			log.Printf("Got global virtual host: %v", vh)
			for _, route := range vh.Routes {
				log.Printf("Got route entry %v (under global virtual host %v): %v", route.Name, vh.Name, route)
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

			for cn, _ := range clusters {
				cluster_names = append(cluster_names, cn)
			}
		}
	}

    // TODO(gu0keno0): better handling of CDS initialization by constructing initial CDS request from local file cache.
	if proxy.cdsInitialized {
        log.Printf("CDS initial request sent already, not going to send further CDS requests for RDS response.")
		return nil
	}

    req := &envoy_service_discovery_v3.DeltaDiscoveryRequest{
		Node:                   proxy.config.AdsClientConfig.Node(),
        TypeUrl:                xds_resource.ClusterType,
        ResourceNamesSubscribe: cluster_names,
    }
    proxy.adsAsyncClient.Send(req)
    proxy.cdsInitialized = true

	return nil
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
			// TODO(gu0keno0): use resource version to avoid duplicated CDS updates.
			proxy.cache.SetResource(xds_resource.ClusterType, cluster.Name, cluster)
			eds_config_names := []string{}
			if cluster_type, ok := cluster.ClusterDiscoveryType.(*envoy_config_cluster_v3.Cluster_Type); ok {
				if cluster_type.Type == envoy_config_cluster_v3.Cluster_EDS {
					if cluster.EdsClusterConfig != nil && cluster.EdsClusterConfig.ServiceName != "" {
						eds_config_names = append(eds_config_names, cluster.EdsClusterConfig.ServiceName)
					}
				}
			}

			// TODO(gu0keno0): better handling of EDS initialization.
			if _, ok := proxy.edsInitialized[cluster.Name]; ok {
				log.Printf("EDS initial request was already sent for cluster %v, not going to resend EDS request for CDS response.", cluster.Name)
				continue
			}

			log.Printf("Got EDS configs: %v\n", eds_config_names)
			req := &envoy_service_discovery_v3.DeltaDiscoveryRequest{
				Node:                   proxy.config.AdsClientConfig.Node(),
				TypeUrl:                xds_resource.EndpointType,
				ResourceNamesSubscribe: eds_config_names,
			}
			proxy.adsAsyncClient.Send(req)
			proxy.edsInitialized[cluster.Name] = struct{}{}
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
		//TODO(gu0keno0): use resource version to avoid duplicated EDS updates.
		log.Printf("Got Cluster Load Assignment: %v\n", cluster_load_assignment)
		proxy.cache.SetResource(xds_resource.EndpointType, cluster_load_assignment.ClusterName, cluster_load_assignment)
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
