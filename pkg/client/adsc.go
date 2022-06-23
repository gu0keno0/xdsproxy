package client

import (
	"context"
	"fmt"
	"math"
	"sync"

	envoy_service_discovery_v3 "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"google.golang.org/grpc"
)

// AdsDeltaStreamClient wraps the incremental ADS client gRPC stub and provides
// security and connection management supports.
// See https://www.envoyproxy.io/docs/envoy/latest/api-docs/xds_protocol about xDS protocol.
type AdsDeltaStreamClient struct {
	config *AdsClientConfig

	mutex  sync.Mutex
	client envoy_service_discovery_v3.AggregatedDiscoveryService_DeltaAggregatedResourcesClient
	conn   *grpc.ClientConn
	cancel context.CancelFunc
}

func NewAdsDeltaStreamClient(config *AdsClientConfig) *AdsDeltaStreamClient {
	adsc := &AdsDeltaStreamClient{}
	adsc.config = config

	return adsc
}

func (adsc *AdsDeltaStreamClient) getClient() (envoy_service_discovery_v3.AggregatedDiscoveryService_DeltaAggregatedResourcesClient, error) {
	adsc.mutex.Lock()
	if adsc.client != nil && adsc.conn != nil {
		client := adsc.client
		adsc.mutex.Unlock()
		return client, nil
	}
	adsc.mutex.Unlock()

	// Dial to ADS server to get gRPC connection.
	// TODO(gu0keno0): add load balancing and TLS supports.
	conn, err := grpc.Dial(
		adsc.config.ServerAddress,
		grpc.WithInsecure(),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(math.MaxInt32)),
	)
	if err != nil {
		return nil, err
	}

	// Create incremental ADS client on top of the gRPC connection to ADS server.
	client := envoy_service_discovery_v3.NewAggregatedDiscoveryServiceClient(conn)
	ctx, cancel := context.WithCancel(context.Background())
	ads_client, err := client.DeltaAggregatedResources(ctx)
	if err != nil {
		if conn != nil {
			conn.Close()
		}
		return nil, err
	}

	adsc.mutex.Lock()
	if adsc.client != nil && adsc.conn != nil {
		client := adsc.client
		adsc.mutex.Unlock()
		// Connection is already established, no need to refresh it.
		cancel()
		conn.Close()
		return client, nil
	}
	// The current goroutine is the first to (re)establish the connection.
	adsc.conn, adsc.client, adsc.cancel = conn, ads_client, cancel
	adsc.mutex.Unlock()

	return ads_client, nil
}

func (adsc *AdsDeltaStreamClient) Send(req *envoy_service_discovery_v3.DeltaDiscoveryRequest) error {
	// TODO(gu0keno0): handle reconnect.
	client, err := adsc.getClient()
	if err != nil {
		return fmt.Errorf("cannot get ADS client for sending requests: %v", err)
	}

	return client.Send(req)
}

func (adsc *AdsDeltaStreamClient) Recv() (*envoy_service_discovery_v3.DeltaDiscoveryResponse, error) {
	// TODO(gu0keno0): handle reconnect.
	client, err := adsc.getClient()
	if err != nil {
		return nil, fmt.Errorf("cannot get ADS client for receiving responses: %v", err)
	}

	return client.Recv()
}

func (adsc *AdsDeltaStreamClient) Disconnect() {
	adsc.mutex.Lock()
	conn, cancel := adsc.conn, adsc.cancel
	adsc.client, adsc.conn, adsc.cancel = nil, nil, nil
	adsc.mutex.Unlock()

	if cancel != nil {
		cancel()
	}
	if conn != nil {
		conn.Close()
	}
}
