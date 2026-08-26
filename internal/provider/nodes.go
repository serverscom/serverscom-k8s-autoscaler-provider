package provider

import (
	"context"

	klog "k8s.io/klog/v2"

	"github.com/serverscom/serverscom-k8s-autoscaler-provider/internal/protos"
)

// Node statuses of the Public API.
const (
	// nodeStatusPending means the order is accepted, no hardware handed out yet.
	nodeStatusPending = "pending"
	// nodeStatusProcessing means provisioning is under way.
	nodeStatusProcessing = "processing"
	// nodeStatusCreated means the node is built but has not joined the cluster yet.
	nodeStatusCreated = "created"
	// nodeStatusActive means the node is in the cluster.
	nodeStatusActive = "active"
	// nodeStatusUpgrading means the node is in the cluster and an upgrade is running.
	nodeStatusUpgrading = "upgrading"
	// nodeStatusRemoved means the node has been released. The API keeps returning such nodes in
	// its listings indefinitely.
	nodeStatusRemoved = "removed"
)

// NodeGroupNodes lists the nodes of the group along with their state.
//
// Released nodes are filtered out. The API keeps returning them forever, and reporting one
// would leave the autoscaler staring at an instance that is eternally being deleted. Dropping
// them also lines the list up with the group's own node counter, which likewise counts
// everything but removed.
func (p *Provider) NodeGroupNodes(ctx context.Context, req *protos.NodeGroupNodesRequest) (*protos.NodeGroupNodesResponse, error) {
	if req.GetId() == "" {
		return nil, statusInvalidArgumentf("node group id is required")
	}

	ctx, cancel := p.callContext(ctx)
	defer cancel()

	nodes, err := p.api.AutoscaleNodeGroupNodes(p.clusterID, req.GetId()).SetPerPage(perPage).Collect(ctx)
	if err != nil {
		klog.V(1).Infof("NodeGroupNodes: cannot list nodes of autoscale node group %s: %v", req.GetId(), err)
		return nil, toGRPCError(ctx, err)
	}

	instances := make([]*protos.Instance, 0, len(nodes))
	for _, node := range nodes {
		if node.Status == nodeStatusRemoved {
			continue
		}

		instances = append(instances, &protos.Instance{
			// The id has to be the providerID: it is the only key the autoscaler has to line
			// this instance up with a Kubernetes node.
			Id: buildProviderID(p.clusterID, node.ID),
			Status: &protos.InstanceStatus{
				InstanceState: instanceState(node.ID, node.Status),
				// Reporting failed instances is out of scope: a node that made it into an order
				// is assumed to arrive eventually.
				ErrorInfo: &protos.InstanceErrorInfo{},
			},
		})
	}

	klog.V(5).Infof("NodeGroupNodes: autoscale node group %s has %d live node(s)", req.GetId(), len(instances))

	return &protos.NodeGroupNodesResponse{Instances: instances}, nil
}

// instanceState maps an API node status onto the gRPC instance state.
//
// "created" is a creating instance rather than a running one on purpose: the node is built but
// has not joined the cluster, and the API refuses to delete it with a conflict until it has.
func instanceState(nodeID, status string) protos.InstanceStatus_InstanceState {
	switch status {
	case nodeStatusPending, nodeStatusProcessing, nodeStatusCreated:
		return protos.InstanceStatus_instanceCreating
	case nodeStatusActive, nodeStatusUpgrading:
		return protos.InstanceStatus_instanceRunning
	default:
		// An unknown status is reported as unspecified rather than guessed at: the autoscaler
		// then treats the instance as having no state information instead of acting on a wrong one.
		klog.V(1).Infof("NodeGroupNodes: node %s has unknown status %q, reporting an unspecified state", nodeID, status)
		return protos.InstanceStatus_unspecified
	}
}
