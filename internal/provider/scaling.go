package provider

import (
	"context"

	serverscom "github.com/serverscom/serverscom-go-client/pkg"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	klog "k8s.io/klog/v2"

	"github.com/serverscom/serverscom-k8s-autoscaler-provider/internal/protos"
)

// NodeGroupIncreaseSize raises the target size of the group and orders the nodes.
//
// The delta is validated before the API is touched: a malformed request must never order
// hardware. The call is made exactly once - increasing the size is not idempotent, so a retry
// on our side would order the hardware twice.
func (p *Provider) NodeGroupIncreaseSize(ctx context.Context, req *protos.NodeGroupIncreaseSizeRequest) (*protos.NodeGroupIncreaseSizeResponse, error) {
	if req.GetId() == "" {
		return nil, statusInvalidArgumentf("node group id is required")
	}

	if req.GetDelta() <= 0 {
		return nil, statusInvalidArgumentf("increase size delta must be positive, got %d", req.GetDelta())
	}

	ctx, cancel := p.callContext(ctx)
	defer cancel()

	input := serverscom.KubernetesClusterAutoscaleNodeGroupIncreaseSizeInput{Delta: int64(req.GetDelta())}

	group, err := p.api.IncreaseAutoscaleNodeGroupSize(ctx, p.clusterID, req.GetId(), input)
	if err != nil {
		klog.V(1).Infof("NodeGroupIncreaseSize: node group %s, delta %d: %v", req.GetId(), req.GetDelta(), err)
		return nil, toGRPCError(ctx, err)
	}

	klog.V(5).Infof("NodeGroupIncreaseSize: node group %s target size is now %d", group.ID, group.TargetNodes)

	return &protos.NodeGroupIncreaseSizeResponse{}, nil
}

// NodeGroupDecreaseTargetSize lowers the target size of the group without touching its nodes.
//
// The autoscaler passes a negative delta ("remove this many from the target"), our endpoint
// takes a positive one ("lower the target by this much"), so the sign is converted explicitly
// here. A non-negative delta is rejected rather than guessed at: the autoscaler never sends
// one, and reading it the wrong way round would raise the target instead of lowering it.
func (p *Provider) NodeGroupDecreaseTargetSize(ctx context.Context, req *protos.NodeGroupDecreaseTargetSizeRequest) (*protos.NodeGroupDecreaseTargetSizeResponse, error) {
	if req.GetId() == "" {
		return nil, statusInvalidArgumentf("node group id is required")
	}

	if req.GetDelta() >= 0 {
		return nil, statusInvalidArgumentf("decrease target size delta must be negative, got %d", req.GetDelta())
	}

	ctx, cancel := p.callContext(ctx)
	defer cancel()

	input := serverscom.KubernetesClusterAutoscaleNodeGroupDecreaseTargetSizeInput{Delta: int64(-req.GetDelta())}

	group, err := p.api.DecreaseAutoscaleNodeGroupTargetSize(ctx, p.clusterID, req.GetId(), input)
	if err != nil {
		klog.V(1).Infof("NodeGroupDecreaseTargetSize: node group %s, delta %d: %v", req.GetId(), req.GetDelta(), err)
		return nil, toGRPCError(ctx, err)
	}

	klog.V(5).Infof("NodeGroupDecreaseTargetSize: node group %s target size is now %d", group.ID, group.TargetNodes)

	return &protos.NodeGroupDecreaseTargetSizeResponse{}, nil
}

// NodeGroupDeleteNodes releases the given nodes and lowers the target size to match.
//
// All the nodes go out in a single request so that the target size moves once. The API answers
// with a conflict for a node that has not joined the cluster yet, and that conflict reaches the
// autoscaler untouched.
func (p *Provider) NodeGroupDeleteNodes(ctx context.Context, req *protos.NodeGroupDeleteNodesRequest) (*protos.NodeGroupDeleteNodesResponse, error) {
	if req.GetId() == "" {
		return nil, statusInvalidArgumentf("node group id is required")
	}

	if len(req.GetNodes()) == 0 {
		return nil, statusInvalidArgumentf("at least one node is required")
	}

	nodeIDs := make([]string, 0, len(req.GetNodes()))
	for _, node := range req.GetNodes() {
		clusterID, nodeID, err := parseAutoscaleProviderID(node.GetProviderID())
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "cannot delete node %q: %v", node.GetName(), err)
		}

		if clusterID != p.clusterID {
			return nil, status.Errorf(codes.InvalidArgument,
				"cannot delete node %q: it belongs to cluster %s, this provider serves cluster %s",
				node.GetName(), clusterID, p.clusterID)
		}

		nodeIDs = append(nodeIDs, nodeID)
	}

	ctx, cancel := p.callContext(ctx)
	defer cancel()

	input := serverscom.KubernetesClusterAutoscaleNodeGroupDeleteNodesInput{NodeIDs: nodeIDs}

	group, err := p.api.DeleteAutoscaleNodeGroupNodes(ctx, p.clusterID, req.GetId(), input)
	if err != nil {
		klog.V(1).Infof("NodeGroupDeleteNodes: node group %s, nodes %v: %v", req.GetId(), nodeIDs, err)
		return nil, toGRPCError(ctx, err)
	}

	klog.V(5).Infof(
		"NodeGroupDeleteNodes: released %d node(s) of node group %s, target size is now %d",
		len(nodeIDs), group.ID, group.TargetNodes)

	return &protos.NodeGroupDeleteNodesResponse{}, nil
}

func statusInvalidArgumentf(format string, args ...any) error {
	return status.Errorf(codes.InvalidArgument, format, args...)
}
