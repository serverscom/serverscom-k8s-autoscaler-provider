package provider

import (
	"context"
	"testing"

	. "github.com/onsi/gomega"
	serverscom "github.com/serverscom/serverscom-go-client/pkg"
	gomock "go.uber.org/mock/gomock"

	"github.com/serverscom/serverscom-k8s-autoscaler-provider/internal/protos"
	serverscom_testing "github.com/serverscom/serverscom-k8s-autoscaler-provider/internal/testing"
)

// expectGroupNodes wires the paginated listing of the nodes of a group.
func expectGroupNodes(t *testing.T, api *serverscom_testing.MockKubernetesClustersService, nodes []serverscom.KubernetesClusterNode) {
	t.Helper()

	ctrl := gomock.NewController(t)
	collection := serverscom_testing.NewMockCollection[serverscom.KubernetesClusterNode](ctrl)

	collection.EXPECT().SetPerPage(perPage).Return(collection)
	collection.EXPECT().Collect(gomock.Any()).Return(nodes, nil)

	api.EXPECT().AutoscaleNodeGroupNodes(testClusterID, testGroupID).Return(collection)
}

func TestNodeGroupNodesStatusMapping(t *testing.T) {
	cases := []struct {
		apiStatus string
		want      protos.InstanceStatus_InstanceState
	}{
		{apiStatus: "pending", want: protos.InstanceStatus_instanceCreating},
		{apiStatus: "processing", want: protos.InstanceStatus_instanceCreating},
		// built, but has not joined the cluster yet - the API refuses to delete it with a
		// conflict until it has, so it is a creating instance, not a running one
		{apiStatus: "created", want: protos.InstanceStatus_instanceCreating},
		{apiStatus: "active", want: protos.InstanceStatus_instanceRunning},
		{apiStatus: "upgrading", want: protos.InstanceStatus_instanceRunning},
		{apiStatus: "something-new", want: protos.InstanceStatus_unspecified},
	}

	for _, tc := range cases {
		t.Run(tc.apiStatus, func(t *testing.T) {
			g := NewGomegaWithT(t)

			p, api := newTestProvider(t)
			expectGroupNodes(t, api, []serverscom.KubernetesClusterNode{{ID: "node1", Status: tc.apiStatus}})

			res, err := p.NodeGroupNodes(context.Background(), &protos.NodeGroupNodesRequest{Id: testGroupID})

			g.Expect(err).To(BeNil())
			g.Expect(res.GetInstances()).To(HaveLen(1))
			g.Expect(res.GetInstances()[0].GetStatus().GetInstanceState()).To(Equal(tc.want))
		})
	}
}

// Released nodes keep being returned by the API listing forever. Reporting one would leave the
// autoscaler staring at an instance that is eternally being deleted.
func TestNodeGroupNodesFiltersRemoved(t *testing.T) {
	g := NewGomegaWithT(t)

	p, api := newTestProvider(t)
	expectGroupNodes(t, api, []serverscom.KubernetesClusterNode{
		{ID: "node1", Status: "active"},
		{ID: "node2", Status: "removed"},
		{ID: "node3", Status: "removed"},
		{ID: "node4", Status: "pending"},
	})

	res, err := p.NodeGroupNodes(context.Background(), &protos.NodeGroupNodesRequest{Id: testGroupID})

	g.Expect(err).To(BeNil())
	g.Expect(res.GetInstances()).To(HaveLen(2))

	ids := []string{res.GetInstances()[0].GetId(), res.GetInstances()[1].GetId()}
	g.Expect(ids).To(ConsistOf(
		buildProviderID(testClusterID, "node1"),
		buildProviderID(testClusterID, "node4"),
	))
}

// The instance id has to be the full providerID: it is the only key the autoscaler has to line
// an instance up with a Kubernetes node.
func TestNodeGroupNodesReportsProviderIDs(t *testing.T) {
	g := NewGomegaWithT(t)

	p, api := newTestProvider(t)
	expectGroupNodes(t, api, []serverscom.KubernetesClusterNode{{ID: "ELe36rd6", Status: "active"}})

	res, err := p.NodeGroupNodes(context.Background(), &protos.NodeGroupNodesRequest{Id: testGroupID})

	g.Expect(err).To(BeNil())
	g.Expect(res.GetInstances()[0].GetId()).
		To(Equal("serverscom://kubernetes-autoscale-node/" + testClusterID + "/ELe36rd6"))
	g.Expect(res.GetInstances()[0].GetStatus().GetErrorInfo().GetErrorCode()).To(BeEmpty())
}

func TestNodeGroupNodesEmptyGroup(t *testing.T) {
	g := NewGomegaWithT(t)

	p, api := newTestProvider(t)
	expectGroupNodes(t, api, nil)

	res, err := p.NodeGroupNodes(context.Background(), &protos.NodeGroupNodesRequest{Id: testGroupID})

	g.Expect(err).To(BeNil())
	g.Expect(res.GetInstances()).To(BeEmpty())
}
