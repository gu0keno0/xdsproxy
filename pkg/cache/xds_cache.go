package cache

import (
	"fmt"

	"github.com/envoyproxy/go-control-plane/pkg/cache/types"
	go_control_plane_cache "github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	xds_resource "github.com/envoyproxy/go-control-plane/pkg/resource/v3"
)

type XdsCache struct {
	go_control_plane_cache.MuxCache
}

var _ go_control_plane_cache.Cache = &XdsCache{}

func NewXdsCache() *XdsCache {
	cache := &XdsCache{}
	cache.Classify = func(req *go_control_plane_cache.Request) string {
		return req.TypeUrl
	}
	cache.ClassifyDelta = func(req *go_control_plane_cache.DeltaRequest) string {
		return req.TypeUrl
	}
	cache.Caches = make(map[string]go_control_plane_cache.Cache)
	cache.Caches[xds_resource.ListenerType] = go_control_plane_cache.NewLinearCache(xds_resource.ListenerType)
	// Route cache holds RouteConfiguration objects.
	cache.Caches[xds_resource.RouteType] = go_control_plane_cache.NewLinearCache(xds_resource.RouteType)
	cache.Caches[xds_resource.ClusterType] = go_control_plane_cache.NewLinearCache(xds_resource.ClusterType)
	// Endpoint cache holds ClusterLoadAssignment objects.
	cache.Caches[xds_resource.EndpointType] = go_control_plane_cache.NewLinearCache(xds_resource.EndpointType)

	return cache
}

func (xc *XdsCache) SetResource(typeUrl string, name string, resource types.Resource) error {
	cache, ok := xc.Caches[typeUrl]
	if !ok {
		return fmt.Errorf("No cache found for typeUrl: %v", typeUrl)
	}

	linearCache := cache.(*go_control_plane_cache.LinearCache)
	// TODO(gu0keno0): add version control to avoid sending duplicated updates.
	linearCache.UpdateResource(name, resource)

	return nil
}
