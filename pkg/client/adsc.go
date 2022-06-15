package client

import (
	"context"
	"math"

	envoy_service_discovery_v3 "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"google.golang.org/grpc"
)

// AdsDeltaStreamClient wraps the incremental ADS client gRPC stub.
// See https://www.envoyproxy.io/docs/envoy/latest/api-docs/xds_protocol about xDS protocol.
type AdsDeltaStreamClient struct {
	client envoy_service_discovery_v3.AggregatedDiscoveryService_DeltaAggregatedResourcesClient
	conn   *grpc.ClientConn
	cancel context.CancelFunc
	config *AdsClientConfig
}

func NewAdsDeltaStreamClient(config *AdsClientConfig) (*AdsDeltaStreamClient, error) {
	adsc := &AdsDeltaStreamClient{}
	adsc.config = config

	// Dial to ADS server to get gRPC connection.
	// TODO(gu0keno0): add load balancing and TLS supports.
	conn, err := grpc.Dial(
		config.ServerAddress,
		grpc.WithInsecure(),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(math.MaxInt32)),
	)
	if err != nil {
		return nil, err
	}
	adsc.conn = conn

	// Create incremental ADS client on top of the gRPC connection to ADS server.
	client := envoy_service_discovery_v3.NewAggregatedDiscoveryServiceClient(adsc.conn)
	ctx, cancel := context.WithCancel(context.Background())
	adsc.cancel = cancel
	adsc.client, err = client.DeltaAggregatedResources(ctx)
	if err != nil {
		if adsc.conn != nil {
			adsc.conn.Close()
		}
		return nil, err
	}

	return adsc, nil
}

func (adsc *AdsDeltaStreamClient) Send(req *envoy_service_discovery_v3.DeltaDiscoveryRequest) error {
	return adsc.client.Send(req)
}

func (adsc *AdsDeltaStreamClient) Recv() (*envoy_service_discovery_v3.DeltaDiscoveryResponse, error) {
	return adsc.client.Recv()
}

func (adsc *AdsDeltaStreamClient) Close() {
	adsc.cancel()
	if adsc.conn != nil {
		adsc.conn.Close()
		adsc.conn = nil
	}
	adsc.conn = nil
}
