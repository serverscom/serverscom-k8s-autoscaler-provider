// Package provider implements the cluster-autoscaler CloudProvider gRPC service on top of the
// Servers.com Public API.
//
// The service is stateless by design: it holds no autoscaling state between calls, does not
// coordinate with other instances of itself, and reads everything it answers with from the API
// on every call. That is what lets several provider instances run side by side while exactly
// one autoscaler is active, and what guarantees that a bound edited through the portal reaches
// the autoscaler as soon as it drops its own cache on Refresh().
//
// It also never retries. Increasing the size of a group is not idempotent - it shifts the
// target size by a delta - so a retry on our side would order the hardware twice. Timeouts,
// broken connections and 5xx responses are returned upwards and the decision is left to the
// autoscaler.
package provider

import (
	"context"
	"fmt"
	"time"

	serverscom "github.com/serverscom/serverscom-go-client/pkg"

	"github.com/serverscom/serverscom-k8s-autoscaler-provider/internal/protos"
)

// perPage is the page size for every paginated listing; it is the maximum the API accepts,
// which keeps the number of round trips per autoscaler loop down.
const perPage = 100

// autoscaleGroupType is the node group type the autoscaler is allowed to touch. Static groups
// carry a different type and are invisible to this provider.
const autoscaleGroupType = "autoscale"

// Provider serves the CloudProvider service for a single Kubernetes cluster.
type Provider struct {
	protos.UnimplementedCloudProviderServer

	api       serverscom.KubernetesClustersService
	clusterID string
	maxPods   int64

	// apiTimeout bounds an API call only when the inbound gRPC call carries no deadline of its
	// own. The autoscaler sends its configured grpc_timeout as a deadline and that one wins.
	apiTimeout time.Duration
}

// New builds a Provider for clusterID. maxPods is the pod capacity reported in the node
// template; the Public API does not expose it.
func New(api serverscom.KubernetesClustersService, clusterID string, maxPods int64, apiTimeout time.Duration) *Provider {
	return &Provider{
		api:        api,
		clusterID:  clusterID,
		maxPods:    maxPods,
		apiTimeout: apiTimeout,
	}
}

// callContext derives the context of a single API call. The autoscaler's deadline, when it
// sends one, is honoured as-is; apiTimeout is only a backstop for callers that send none.
func (p *Provider) callContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return context.WithCancel(ctx)
	}

	return context.WithTimeout(ctx, p.apiTimeout)
}

// pbNodeGroup converts an API autoscale node group to its gRPC counterpart.
//
// The autoscaler freezes MinSize, MaxSize and Debug on the NodeGroup object it builds out of
// this message and keeps them for the lifetime of that object, so the values have to be the
// ones the API holds right now.
func pbNodeGroup(group *serverscom.KubernetesClusterAutoscaleNodeGroup) *protos.NodeGroup {
	return &protos.NodeGroup{
		Id:      group.ID,
		MinSize: int32(group.MinNodes),
		MaxSize: int32(group.MaxNodes),
		Debug:   debugString(group),
	}
}

func debugString(group *serverscom.KubernetesClusterAutoscaleNodeGroup) string {
	return fmt.Sprintf(
		"id=%s name=%s node_type=%s min=%d max=%d target=%d current=%d",
		group.ID, group.Name, group.NodeType,
		group.MinNodes, group.MaxNodes, group.TargetNodes, group.CurrentNodes,
	)
}

// Provider implements every RPC of the CloudProvider service; the ones that are out of scope
// answer Unimplemented rather than being left to the embedded default.
var _ protos.CloudProviderServer = (*Provider)(nil)
