package client

import (
	"errors"
	"sync"
	"time"

	envoy_service_discovery_v3 "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
)

// AsyncClientCallback is the client side callback function for handling Incremental ADS responses.
type AsyncClientCallback interface {
	HandleADS(client *AsyncClient, resp *envoy_service_discovery_v3.DeltaDiscoveryResponse) error
}

type resourceState struct {
	version   string
	respNonce string
}

type aggResourceState struct {
	resourceStates map[string]resourceState
}

// AsyncClient is an asynchronous Incremental ADS client,
// It uses goroutines and channels to handle requests and responses for the
// underlying gRPC Incremental ADS client asynchronously.
type AsyncClient struct {
	config   *AdsClientConfig
	callback AsyncClientCallback

	mutex    sync.Mutex
	adsc     *AdsDeltaStreamClient
	adsState map[string]aggResourceState
	reqChan  chan *envoy_service_discovery_v3.DeltaDiscoveryRequest
	stopChan chan struct{}
}

func NewAsyncClient(config *AdsClientConfig, cb AsyncClientCallback) (*AsyncClient, error) {
	asc := &AsyncClient{
		config:   config,
		callback: cb,
		adsState: map[string]aggResourceState{},
		reqChan:  make(chan *envoy_service_discovery_v3.DeltaDiscoveryRequest, config.MaxPendingRequests),
		stopChan: make(chan struct{}),
	}

	adsc, err := NewAdsDeltaStreamClient(asc.config)
	if err != nil {
		return nil, err
	}
	asc.adsc = adsc
	return asc, nil
}

func (asc *AsyncClient) getAdsClient() *AdsDeltaStreamClient {
	asc.mutex.Lock()
	defer asc.mutex.Unlock()
	return asc.adsc
}

func (asc *AsyncClient) getRequestChan() chan *envoy_service_discovery_v3.DeltaDiscoveryRequest {
	asc.mutex.Lock()
	defer asc.mutex.Unlock()
	return asc.reqChan
}

func (asc *AsyncClient) requestsLoop() {
	for {
		reqChan := asc.getRequestChan()
		if reqChan == nil {
			break
		}
		select {
		case req, ok := <-reqChan:
			if !ok {
				break
			}
			adsc := asc.getAdsClient()
			if adsc == nil {
				break
			}
			adsc.Send(req)
		}
	}
}

func (asc *AsyncClient) responsesLoop() {
	for {
		adsc := asc.getAdsClient()
		if adsc == nil {
			break
		}
		resp, err := adsc.Recv()
		if err != nil {
			time.Sleep(asc.config.RecvErrBackoffInterval)
			continue
		}
		ack := &envoy_service_discovery_v3.DeltaDiscoveryRequest{
			Node:                     asc.config.Node(),
			TypeUrl:                  resp.TypeUrl,
			ResponseNonce:            resp.Nonce,
		}
		// TODO (gu0keno0): implement / rule out the cases for sending NACKs.
		asc.Send(ack)
		asc.callback.HandleADS(asc, resp)
	}
}

func (asc *AsyncClient) Send(req *envoy_service_discovery_v3.DeltaDiscoveryRequest) error {
	reqChan := asc.getRequestChan()
	if reqChan == nil {
		return errors.New("Client is already stopped")
	}

	select {
	case <-asc.stopChan:
		return errors.New("Client is already stopped")
	case reqChan <- req:
		return nil
	default:
		return errors.New("Max pending requests is reached")
	}
}

func (asc *AsyncClient) Run() {
	go asc.requestsLoop()
	go asc.responsesLoop()
}

// Stop permanently terminates an AsyncClient.
func (asc *AsyncClient) Stop() {
	asc.mutex.Lock()
	close(asc.stopChan)
	adsc, reqChan := asc.adsc, asc.reqChan
	asc.adsc, asc.reqChan = nil, nil
	asc.mutex.Unlock()

	if reqChan != nil {
		// TODO(gu0keno0): do we need to drain pending requests?
		close(asc.reqChan)
	}

	if adsc != nil {
		adsc.Close()
	}
}
