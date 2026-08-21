package sub2api

import (
	"context"
	"errors"
	"sort"

	"github.com/Wei-Shaw/sub2api/internal/runtimebridge"
	v1 "github.com/Wei-Shaw/sub2api/pkg/runtimebridge/v1"
)

var (
	ErrRegistryUnavailable = errors.New("sub2api driver registry is unavailable")
	ErrDuplicateEndpoint   = errors.New("sub2api driver endpoint is already registered")
	ErrInvalidEndpoint     = errors.New("sub2api driver endpoint is invalid")
	ErrInvalidExecutor     = errors.New("sub2api driver executor is invalid")
)

// DeferredEndpointFamilies makes the migration boundary explicit. These
// endpoints remain on the legacy compatibility executor until their own pure
// exchange implementation and conformance tests are complete; no new-driver
// failure silently falls back to them.
var DeferredEndpointFamilies = []v1.Endpoint{
	v1.EndpointCountTokens,
	v1.EndpointEmbeddings,
	v1.EndpointAlphaSearch,
	v1.EndpointGeminiNative,
	v1.EndpointImages,
	v1.EndpointVideos,
	v1.EndpointLive,
	v1.EndpointWebSocket,
}

type EndpointExecutor interface {
	Execute(context.Context, v1.Request, runtimebridge.EventSink) (v1.Result, error)
}

type Registry struct {
	executors map[v1.Endpoint]EndpointExecutor
}

func NewRegistry() *Registry {
	return &Registry{executors: make(map[v1.Endpoint]EndpointExecutor)}
}

func (r *Registry) Register(endpoint v1.Endpoint, executor EndpointExecutor) error {
	if r == nil || r.executors == nil {
		return ErrRegistryUnavailable
	}
	if !isRegisteredEndpoint(endpoint) {
		return ErrInvalidEndpoint
	}
	if executor == nil {
		return ErrInvalidExecutor
	}
	if _, exists := r.executors[endpoint]; exists {
		return ErrDuplicateEndpoint
	}
	r.executors[endpoint] = executor
	return nil
}

func (r *Registry) Endpoints() []v1.Endpoint {
	if r == nil {
		return nil
	}
	endpoints := make([]v1.Endpoint, 0, len(r.executors))
	for endpoint := range r.executors {
		endpoints = append(endpoints, endpoint)
	}
	sort.Slice(endpoints, func(i, j int) bool { return endpoints[i] < endpoints[j] })
	return endpoints
}

func (r *Registry) Adapter() (*Adapter, error) {
	if r == nil || len(r.executors) == 0 {
		return nil, ErrRegistryUnavailable
	}
	cloned := make(map[v1.Endpoint]EndpointExecutor, len(r.executors))
	for endpoint, executor := range r.executors {
		cloned[endpoint] = executor
	}
	return &Adapter{executors: cloned}, nil
}

func isRegisteredEndpoint(endpoint v1.Endpoint) bool {
	switch endpoint {
	case v1.EndpointMessages,
		v1.EndpointChatCompletions,
		v1.EndpointResponses,
		v1.EndpointGeminiNative,
		v1.EndpointEmbeddings,
		v1.EndpointAlphaSearch,
		v1.EndpointImages,
		v1.EndpointVideos,
		v1.EndpointCountTokens,
		v1.EndpointLive,
		v1.EndpointWebSocket:
		return true
	default:
		return false
	}
}
