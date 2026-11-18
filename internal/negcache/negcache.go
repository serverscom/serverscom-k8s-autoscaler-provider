// Package negcache is a negative cache for the read-only RPCs of the CloudProvider service.
//
// The autoscaler caches the answers of NodeGroups and NodeGroupForNode until its next Refresh,
// but it never caches an error. While the Public API is failing - 5xx, 429, a broken token, a
// timeout - every lookup the autoscaler makes within one loop turns into another API call, and
// a single loop may ask for the same thing a dozen times.
//
// The cache sits in front of the provider as a gRPC interceptor and remembers the error a
// read-only RPC returned, keyed by the RPC and the object it asked about, until the next
// Refresh. A repeated request within the same loop gets the remembered error back without a
// call to the API. Only errors are kept: a successful answer always comes from the API, so the
// provider still never serves stale bounds.
//
// The mutating RPCs are never cached. A conflict there - the group is at its maximum, a node
// has not joined yet - describes the state of the moment, and an owner raising the maximum
// must take effect on the very next request.
//
// None of this is a retry: the cache only ever removes API calls, it never adds one.
package negcache

import (
	"context"
	"path"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/serverscom/serverscom-k8s-autoscaler-provider/internal/protos"
)

const (
	resultHit  = "hit"
	resultMiss = "miss"
)

// keyFuncs lists the RPCs whose errors are cached, each with the function that extracts the
// object the request is about. Two requests with the same key would cause the same API calls.
var keyFuncs = map[string]func(req any) string{
	protos.CloudProvider_NodeGroups_FullMethodName: func(any) string { return "" },
	protos.CloudProvider_NodeGroupForNode_FullMethodName: func(req any) string {
		// The request carries the whole node, but the provider looks at its providerID only.
		return req.(*protos.NodeGroupForNodeRequest).GetNode().GetProviderID()
	},
	protos.CloudProvider_NodeGroupTargetSize_FullMethodName: func(req any) string {
		return req.(*protos.NodeGroupTargetSizeRequest).GetId()
	},
	protos.CloudProvider_NodeGroupNodes_FullMethodName: func(req any) string {
		return req.(*protos.NodeGroupNodesRequest).GetId()
	},
	protos.CloudProvider_NodeGroupTemplateNodeInfo_FullMethodName: func(req any) string {
		return req.(*protos.NodeGroupTemplateNodeInfoRequest).GetId()
	},
}

type entryKey struct {
	method string
	id     string
}

// Cache holds the errors of the read-only RPCs until the next Refresh.
type Cache struct {
	mu      sync.Mutex
	entries map[entryKey]error

	// generation is bumped by every Refresh. An RPC records it before calling the provider and
	// stores its error only if no Refresh happened meanwhile, so an error from the previous loop
	// never leaks into the next one.
	generation uint64

	lookups *prometheus.CounterVec
}

// New builds an empty Cache and registers its metrics with reg.
func New(reg prometheus.Registerer) *Cache {
	lookups := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "serverscom_autoscaler_provider",
		Subsystem: "negative_cache",
		Name:      "lookups_total",
		Help: "Lookups of the negative cache by RPC. A hit returned a cached error without " +
			"calling the API, a miss called the provider.",
	}, []string{"rpc", "result"})

	// Every series exists from the start, so a rate() over it never sees a missing one.
	for method := range keyFuncs {
		lookups.WithLabelValues(rpcName(method), resultHit)
		lookups.WithLabelValues(rpcName(method), resultMiss)
	}

	reg.MustRegister(lookups)

	return &Cache{
		entries: map[entryKey]error{},
		lookups: lookups,
	}
}

// UnaryServerInterceptor serves cached errors of the read-only RPCs and drops the cache on
// Refresh. Every other RPC passes through untouched.
func (c *Cache) UnaryServerInterceptor(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	if info.FullMethod == protos.CloudProvider_Refresh_FullMethodName {
		c.clear()
		return handler(ctx, req)
	}

	keyFunc, ok := keyFuncs[info.FullMethod]
	if !ok {
		return handler(ctx, req)
	}

	key := entryKey{method: info.FullMethod, id: keyFunc(req)}

	generation, cached := c.lookup(key)
	if cached != nil {
		c.lookups.WithLabelValues(rpcName(info.FullMethod), resultHit).Inc()
		return nil, cached
	}

	c.lookups.WithLabelValues(rpcName(info.FullMethod), resultMiss).Inc()

	resp, err := handler(ctx, req)
	if err != nil && status.Code(err) != codes.Canceled {
		// A cancelled call says nothing about the API - the autoscaler gave up on it - so the
		// next request must reach the API.
		c.store(key, err, generation)
	}

	return resp, err
}

// lookup returns the current generation and the cached error for key, nil if there is none.
// Only non-nil errors are ever stored, so nil means a miss.
func (c *Cache) lookup(key entryKey) (uint64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.generation, c.entries[key]
}

func (c *Cache) store(key entryKey, err error, generation uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if generation != c.generation {
		return
	}

	c.entries[key] = err
}

func (c *Cache) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.generation++
	clear(c.entries)
}

// rpcName turns a full gRPC method name into the bare RPC name used as a metric label.
func rpcName(fullMethod string) string {
	return path.Base(fullMethod)
}
