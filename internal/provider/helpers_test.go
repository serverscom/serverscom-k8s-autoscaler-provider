package provider

import (
	"testing"
	"time"

	serverscom "github.com/serverscom/serverscom-go-client/pkg"
	gomock "go.uber.org/mock/gomock"

	serverscom_testing "github.com/serverscom/serverscom-k8s-autoscaler-provider/internal/testing"
)

const (
	testClusterID = "MYerYkdO"
	testGroupID   = "4zbqjrdp"
	testMaxPods   = 110
)

// newTestProvider wires a Provider onto a fresh mock of the API service.
func newTestProvider(t *testing.T) (*Provider, *serverscom_testing.MockKubernetesClustersService) {
	t.Helper()

	ctrl := gomock.NewController(t)
	api := serverscom_testing.NewMockKubernetesClustersService(ctrl)

	return New(api, testClusterID, testMaxPods, time.Second), api
}

func testGroup(minNodes, maxNodes, target, current int64) *serverscom.KubernetesClusterAutoscaleNodeGroup {
	return &serverscom.KubernetesClusterAutoscaleNodeGroup{
		ID:           testGroupID,
		Name:         "node-group-97",
		Type:         autoscaleGroupType,
		NodeType:     "sbm",
		MinNodes:     minNodes,
		MaxNodes:     maxNodes,
		TargetNodes:  target,
		CurrentNodes: current,
	}
}
