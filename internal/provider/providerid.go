package provider

import (
	"fmt"
	"strings"
)

const (
	// providerName is the cloud prefix of a Kubernetes providerID. It has to match what the
	// cloud-controller-manager stamps onto the node, since providerID is the only key the
	// autoscaler has to line a Kubernetes node up with an instance we report.
	providerName = "serverscom"

	// autoscaleNodeType is the node type segment of the providerID of an autoscale node.
	autoscaleNodeType = "kubernetes-autoscale-node"

	providerIDPrefix = providerName + "://"
)

// buildProviderID renders the providerID of an autoscale node:
//
//	serverscom://kubernetes-autoscale-node/<cluster_id>/<node_id>
//
// Unlike the other node types, an autoscale node is not a standalone resource and can only be
// addressed by the cluster ID + node ID pair, hence the extra segment.
func buildProviderID(clusterID, nodeID string) string {
	return providerIDPrefix + autoscaleNodeType + "/" + clusterID + "/" + nodeID
}

// parseAutoscaleProviderID extracts the cluster and node IDs from an autoscale node providerID.
//
// Anything else - an empty value, another cloud, a master or a node of a static group - is
// rejected with an error. Callers decide what that means; for NodeGroupForNode it means "not
// managed by the autoscaler", for DeleteNodes it is a bad request.
func parseAutoscaleProviderID(providerID string) (clusterID, nodeID string, err error) {
	if !strings.HasPrefix(providerID, providerIDPrefix) {
		return "", "", fmt.Errorf("providerID %q is missing the %q prefix", providerID, providerIDPrefix)
	}

	parts := strings.Split(strings.TrimPrefix(providerID, providerIDPrefix), "/")

	// the node type segment is normalized to dashes: API type strings are snake_case
	// (e.g. "kubernetes_sbm_node"), while the providerID node types are dashed
	if len(parts) == 0 || strings.ReplaceAll(parts[0], "_", "-") != autoscaleNodeType {
		return "", "", fmt.Errorf("providerID %q is not an autoscale node", providerID)
	}

	if len(parts) != 3 {
		return "", "", fmt.Errorf(
			"providerID %q is malformed, expected %s%s/<cluster_id>/<node_id>",
			providerID, providerIDPrefix, autoscaleNodeType)
	}

	if parts[1] == "" || parts[2] == "" {
		return "", "", fmt.Errorf("providerID %q has an empty cluster or node ID", providerID)
	}

	return parts[1], parts[2], nil
}
