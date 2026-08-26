package provider

import (
	"context"

	klog "k8s.io/klog/v2"

	"github.com/serverscom/serverscom-k8s-autoscaler-provider/internal/protos"
)

// NodeGroups lists the autoscale node groups of the cluster.
//
// Static node groups are not returned by this endpoint at all, so the autoscaler never sees
// them and cannot touch them.
func (p *Provider) NodeGroups(ctx context.Context, _ *protos.NodeGroupsRequest) (*protos.NodeGroupsResponse, error) {
	ctx, cancel := p.callContext(ctx)
	defer cancel()

	groups, err := p.api.AutoscaleNodeGroups(p.clusterID).SetPerPage(perPage).Collect(ctx)
	if err != nil {
		klog.V(1).Infof("NodeGroups: cannot list autoscale node groups of cluster %s: %v", p.clusterID, err)
		return nil, toGRPCError(ctx, err)
	}

	pbGroups := make([]*protos.NodeGroup, 0, len(groups))
	for i := range groups {
		pbGroups = append(pbGroups, pbNodeGroup(&groups[i]))
	}

	klog.V(5).Infof("NodeGroups: cluster %s has %d autoscale node group(s)", p.clusterID, len(pbGroups))

	return &protos.NodeGroupsResponse{NodeGroups: pbGroups}, nil
}

// NodeGroupForNode resolves which autoscale group a cluster node belongs to.
//
// A node group with an empty id tells the autoscaler the node is none of its business. That is
// the answer for masters, for nodes of static groups, for nodes of another cluster and for
// nodes whose providerID we cannot read - all of them are normal, so none of them is an error.
func (p *Provider) NodeGroupForNode(ctx context.Context, req *protos.NodeGroupForNodeRequest) (*protos.NodeGroupForNodeResponse, error) {
	node := req.GetNode()
	if node == nil {
		return nil, statusInvalidArgumentf("node is required")
	}

	providerID := node.GetProviderID()

	clusterID, nodeID, err := parseAutoscaleProviderID(providerID)
	if err != nil {
		klog.V(5).Infof("NodeGroupForNode: node %q is not an autoscale node: %v", node.GetName(), err)
		return unmanagedNodeGroup(), nil
	}

	if clusterID != p.clusterID {
		klog.V(1).Infof(
			"NodeGroupForNode: node %q belongs to cluster %s, this provider serves cluster %s",
			node.GetName(), clusterID, p.clusterID)
		return unmanagedNodeGroup(), nil
	}

	ctx, cancel := p.callContext(ctx)
	defer cancel()

	apiNode, err := p.api.GetNode(ctx, p.clusterID, nodeID)
	if err != nil {
		if isNotFound(err) {
			// The node is gone from the API. Reporting no group keeps the autoscaler from
			// acting on something that no longer exists.
			klog.V(5).Infof("NodeGroupForNode: node %s is not in cluster %s any more", nodeID, p.clusterID)
			return unmanagedNodeGroup(), nil
		}

		klog.V(1).Infof("NodeGroupForNode: cannot get node %s of cluster %s: %v", nodeID, p.clusterID, err)
		return nil, toGRPCError(ctx, err)
	}

	if apiNode.Status == nodeStatusRemoved {
		klog.V(5).Infof("NodeGroupForNode: node %s is released, reporting no node group", nodeID)
		return unmanagedNodeGroup(), nil
	}

	if apiNode.NodeGroup.Type != autoscaleGroupType {
		klog.V(5).Infof(
			"NodeGroupForNode: node %s belongs to %s node group %s, not an autoscale one",
			nodeID, apiNode.NodeGroup.Type, apiNode.NodeGroup.ID)
		return unmanagedNodeGroup(), nil
	}

	// The bounds have to come from the group itself: the autoscaler caches min/max/debug on the
	// NodeGroup object it builds out of this response for the whole lifetime of that object.
	group, err := p.api.GetAutoscaleNodeGroup(ctx, p.clusterID, apiNode.NodeGroup.ID)
	if err != nil {
		klog.V(1).Infof(
			"NodeGroupForNode: cannot get autoscale node group %s of cluster %s: %v",
			apiNode.NodeGroup.ID, p.clusterID, err)
		return nil, toGRPCError(ctx, err)
	}

	return &protos.NodeGroupForNodeResponse{NodeGroup: pbNodeGroup(group)}, nil
}

// NodeGroupTargetSize returns the node count the autoscaler has asked the group for.
func (p *Provider) NodeGroupTargetSize(ctx context.Context, req *protos.NodeGroupTargetSizeRequest) (*protos.NodeGroupTargetSizeResponse, error) {
	if req.GetId() == "" {
		return nil, statusInvalidArgumentf("node group id is required")
	}

	ctx, cancel := p.callContext(ctx)
	defer cancel()

	group, err := p.api.GetAutoscaleNodeGroup(ctx, p.clusterID, req.GetId())
	if err != nil {
		klog.V(1).Infof("NodeGroupTargetSize: cannot get autoscale node group %s: %v", req.GetId(), err)
		return nil, toGRPCError(ctx, err)
	}

	return &protos.NodeGroupTargetSizeResponse{TargetSize: int32(group.TargetNodes)}, nil
}

// unmanagedNodeGroup builds the "this node is not autoscaled" answer: a node group with an
// empty id, which is what the contract defines for a node the autoscaler should not process.
func unmanagedNodeGroup() *protos.NodeGroupForNodeResponse {
	return &protos.NodeGroupForNodeResponse{NodeGroup: &protos.NodeGroup{}}
}
