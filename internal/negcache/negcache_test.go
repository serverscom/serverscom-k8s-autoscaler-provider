package negcache

import (
	"context"
	"testing"

	. "github.com/onsi/gomega"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/serverscom/serverscom-k8s-autoscaler-provider/internal/protos"
)

// countingHandler stands in for the provider: it counts the calls that would have reached the
// API and answers each with err.
type countingHandler struct {
	calls int
	err   error
}

func (h *countingHandler) handle(context.Context, any) (any, error) {
	h.calls++
	if h.err != nil {
		return nil, h.err
	}
	return &protos.NodeGroupsResponse{}, nil
}

func newTestCache() *Cache {
	return New(prometheus.NewRegistry())
}

func call(c *Cache, method string, req any, h grpc.UnaryHandler) (any, error) {
	return c.UnaryServerInterceptor(context.Background(), req, &grpc.UnaryServerInfo{FullMethod: method}, h)
}

func refresh(c *Cache) {
	_, _ = call(c, protos.CloudProvider_Refresh_FullMethodName, &protos.RefreshRequest{},
		func(context.Context, any) (any, error) { return &protos.RefreshResponse{}, nil })
}

func lookups(c *Cache, method, result string) float64 {
	return testutil.ToFloat64(c.lookups.WithLabelValues(rpcName(method), result))
}

func unavailable() error {
	return status.Error(codes.Unavailable, "servers.com API internal error: INTERNAL: boom")
}

// Each read-only RPC, asked the same thing twice within one loop, reaches the API once and gets
// the same error back the second time.
func TestReadRPCErrorIsServedFromCache(t *testing.T) {
	cases := []struct {
		method string
		req    any
	}{
		{protos.CloudProvider_NodeGroups_FullMethodName, &protos.NodeGroupsRequest{}},
		{protos.CloudProvider_NodeGroupForNode_FullMethodName, &protos.NodeGroupForNodeRequest{
			Node: &protos.ExternalGrpcNode{ProviderID: "serverscom://kubernetes-autoscale-node/c/n1"},
		}},
		{protos.CloudProvider_NodeGroupTargetSize_FullMethodName, &protos.NodeGroupTargetSizeRequest{Id: "g1"}},
		{protos.CloudProvider_NodeGroupNodes_FullMethodName, &protos.NodeGroupNodesRequest{Id: "g1"}},
		{protos.CloudProvider_NodeGroupTemplateNodeInfo_FullMethodName, &protos.NodeGroupTemplateNodeInfoRequest{Id: "g1"}},
	}

	for _, tc := range cases {
		t.Run(rpcName(tc.method), func(t *testing.T) {
			g := NewGomegaWithT(t)

			c := newTestCache()
			h := &countingHandler{err: unavailable()}

			_, first := call(c, tc.method, tc.req, h.handle)
			_, second := call(c, tc.method, tc.req, h.handle)

			g.Expect(h.calls).To(Equal(1))
			g.Expect(first).To(HaveOccurred())
			g.Expect(second).To(BeIdenticalTo(first))
			g.Expect(lookups(c, tc.method, resultMiss)).To(Equal(1.0))
			g.Expect(lookups(c, tc.method, resultHit)).To(Equal(1.0))
		})
	}
}

// Refresh opens a new autoscaler loop, and the first request of that loop goes to the API.
func TestRefreshDropsCachedErrors(t *testing.T) {
	g := NewGomegaWithT(t)

	c := newTestCache()
	h := &countingHandler{err: unavailable()}
	req := &protos.NodeGroupsRequest{}

	_, _ = call(c, protos.CloudProvider_NodeGroups_FullMethodName, req, h.handle)
	refresh(c)
	_, _ = call(c, protos.CloudProvider_NodeGroups_FullMethodName, req, h.handle)

	g.Expect(h.calls).To(Equal(2))
	g.Expect(lookups(c, protos.CloudProvider_NodeGroups_FullMethodName, resultHit)).To(Equal(0.0))
}

// An RPC that started before a Refresh and failed after it must not poison the new loop.
func TestErrorFromPreviousLoopIsNotStored(t *testing.T) {
	g := NewGomegaWithT(t)

	c := newTestCache()
	req := &protos.NodeGroupsRequest{}

	_, err := call(c, protos.CloudProvider_NodeGroups_FullMethodName, req,
		func(context.Context, any) (any, error) {
			refresh(c)
			return nil, unavailable()
		})
	g.Expect(err).To(HaveOccurred())

	h := &countingHandler{}
	_, err = call(c, protos.CloudProvider_NodeGroups_FullMethodName, req, h.handle)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(h.calls).To(Equal(1))
}

// The key is the object the request is about: an error for one group says nothing about another.
func TestErrorsAreKeyedByObject(t *testing.T) {
	g := NewGomegaWithT(t)

	c := newTestCache()
	h := &countingHandler{err: unavailable()}

	_, _ = call(c, protos.CloudProvider_NodeGroupNodes_FullMethodName, &protos.NodeGroupNodesRequest{Id: "g1"}, h.handle)
	_, _ = call(c, protos.CloudProvider_NodeGroupNodes_FullMethodName, &protos.NodeGroupNodesRequest{Id: "g2"}, h.handle)
	// Same id, different RPC.
	_, _ = call(c, protos.CloudProvider_NodeGroupTargetSize_FullMethodName, &protos.NodeGroupTargetSizeRequest{Id: "g1"}, h.handle)

	g.Expect(h.calls).To(Equal(3))
}

// NodeGroupForNode is keyed by providerID only: the rest of the node does not change the calls
// the provider makes.
func TestNodeGroupForNodeIsKeyedByProviderID(t *testing.T) {
	g := NewGomegaWithT(t)

	c := newTestCache()
	h := &countingHandler{err: unavailable()}
	providerID := "serverscom://kubernetes-autoscale-node/c/n1"

	_, _ = call(c, protos.CloudProvider_NodeGroupForNode_FullMethodName, &protos.NodeGroupForNodeRequest{
		Node: &protos.ExternalGrpcNode{ProviderID: providerID, Name: "a"},
	}, h.handle)
	_, _ = call(c, protos.CloudProvider_NodeGroupForNode_FullMethodName, &protos.NodeGroupForNodeRequest{
		Node: &protos.ExternalGrpcNode{ProviderID: providerID, Name: "b", Labels: map[string]string{"x": "y"}},
	}, h.handle)

	g.Expect(h.calls).To(Equal(1))
}

// Only errors are cached; a successful answer always comes from the API.
func TestSuccessIsNotCached(t *testing.T) {
	g := NewGomegaWithT(t)

	c := newTestCache()
	h := &countingHandler{}

	_, _ = call(c, protos.CloudProvider_NodeGroups_FullMethodName, &protos.NodeGroupsRequest{}, h.handle)
	_, _ = call(c, protos.CloudProvider_NodeGroups_FullMethodName, &protos.NodeGroupsRequest{}, h.handle)

	g.Expect(h.calls).To(Equal(2))
	g.Expect(lookups(c, protos.CloudProvider_NodeGroups_FullMethodName, resultMiss)).To(Equal(2.0))
}

// A cancelled call is the autoscaler giving up, not the API failing.
func TestCanceledIsNotCached(t *testing.T) {
	g := NewGomegaWithT(t)

	c := newTestCache()
	h := &countingHandler{err: status.Error(codes.Canceled, "call cancelled")}

	_, _ = call(c, protos.CloudProvider_NodeGroups_FullMethodName, &protos.NodeGroupsRequest{}, h.handle)
	_, _ = call(c, protos.CloudProvider_NodeGroups_FullMethodName, &protos.NodeGroupsRequest{}, h.handle)

	g.Expect(h.calls).To(Equal(2))
}

// Every error other than Canceled is cached - a 401 within one loop is as pointless to repeat as
// a 5xx.
func TestAnyOtherErrorIsCached(t *testing.T) {
	for _, code := range []codes.Code{
		codes.NotFound, codes.Unavailable, codes.DeadlineExceeded, codes.Unauthenticated,
		codes.PermissionDenied, codes.InvalidArgument, codes.Internal,
	} {
		t.Run(code.String(), func(t *testing.T) {
			g := NewGomegaWithT(t)

			c := newTestCache()
			h := &countingHandler{err: status.Error(code, "boom")}

			_, _ = call(c, protos.CloudProvider_NodeGroups_FullMethodName, &protos.NodeGroupsRequest{}, h.handle)
			_, _ = call(c, protos.CloudProvider_NodeGroups_FullMethodName, &protos.NodeGroupsRequest{}, h.handle)

			g.Expect(h.calls).To(Equal(1))
		})
	}
}

// The mutating RPCs always reach the provider: their conflicts describe the state of the moment,
// and a raised maximum has to take effect on the very next request.
func TestMutatingRPCsAreNotCached(t *testing.T) {
	cases := []struct {
		method string
		req    any
	}{
		{protos.CloudProvider_NodeGroupIncreaseSize_FullMethodName, &protos.NodeGroupIncreaseSizeRequest{Id: "g1", Delta: 1}},
		{protos.CloudProvider_NodeGroupDecreaseTargetSize_FullMethodName, &protos.NodeGroupDecreaseTargetSizeRequest{Id: "g1", Delta: -1}},
		{protos.CloudProvider_NodeGroupDeleteNodes_FullMethodName, &protos.NodeGroupDeleteNodesRequest{Id: "g1"}},
	}

	for _, tc := range cases {
		t.Run(rpcName(tc.method), func(t *testing.T) {
			g := NewGomegaWithT(t)

			c := newTestCache()
			h := &countingHandler{err: status.Error(codes.FailedPrecondition, "conflict: MAXIMUM_NODES_REACHED")}

			_, _ = call(c, tc.method, tc.req, h.handle)
			_, _ = call(c, tc.method, tc.req, h.handle)

			g.Expect(h.calls).To(Equal(2))
		})
	}
}

// Every cached RPC has both series from the start.
func TestMetricsArePreinitialised(t *testing.T) {
	g := NewGomegaWithT(t)

	c := newTestCache()

	g.Expect(testutil.CollectAndCount(c.lookups)).To(Equal(2 * len(keyFuncs)))
}
