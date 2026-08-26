package provider

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klog "k8s.io/klog/v2"

	"github.com/serverscom/serverscom-k8s-autoscaler-provider/internal/protos"
)

// bytesPerGB converts the RAM size the API reports, which is in gigabytes.
const bytesPerGB = 1024 * 1024 * 1024

// NodeGroupTemplateNodeInfo describes what a node of the group would look like, so that the
// autoscaler can simulate a scale-up before asking for one.
//
// The template carries capacity only: cpu from the logical CPU count, memory from the RAM size
// and the configured pod capacity. Allocatable mirrors capacity because the scale-up simulation
// bin-packs against allocatable, and a node with none of it would never be picked.
//
// Deliberately absent: labels and taints. The consequence is accepted - pods that select nodes
// through nodeSelector or affinity on labels will not trigger a scale-up of such a group.
func (p *Provider) NodeGroupTemplateNodeInfo(ctx context.Context, req *protos.NodeGroupTemplateNodeInfoRequest) (*protos.NodeGroupTemplateNodeInfoResponse, error) {
	if req.GetId() == "" {
		return nil, statusInvalidArgumentf("node group id is required")
	}

	ctx, cancel := p.callContext(ctx)
	defer cancel()

	template, err := p.api.GetAutoscaleNodeGroupTemplate(ctx, p.clusterID, req.GetId())
	if err != nil {
		klog.V(1).Infof("NodeGroupTemplateNodeInfo: cannot get the template of node group %s: %v", req.GetId(), err)
		return nil, toGRPCError(ctx, err)
	}

	// An unknown shape is an error rather than Unimplemented: Unimplemented would be cached by
	// the autoscaler for the whole lifetime of the node group object, an error is retried.
	if template.LogicalCPUCount == nil || template.RAMSize == nil {
		return nil, status.Errorf(codes.FailedPrecondition,
			"autoscale node group %s has no hardware shape yet (flavor %q)", req.GetId(), template.FlavorName)
	}

	capacity := apiv1.ResourceList{
		apiv1.ResourceCPU:    *resource.NewQuantity(*template.LogicalCPUCount, resource.DecimalSI),
		apiv1.ResourceMemory: *resource.NewQuantity(*template.RAMSize*bytesPerGB, resource.BinarySI),
		apiv1.ResourcePods:   *resource.NewQuantity(p.maxPods, resource.DecimalSI),
	}

	node := &apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: req.GetId() + "-template",
		},
		Status: apiv1.NodeStatus{
			Capacity:    capacity,
			Allocatable: capacity.DeepCopy(),
		},
	}

	nodeBytes, err := node.Marshal()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "cannot serialize the node template of group %s: %v", req.GetId(), err)
	}

	return &protos.NodeGroupTemplateNodeInfoResponse{NodeBytes: nodeBytes}, nil
}
