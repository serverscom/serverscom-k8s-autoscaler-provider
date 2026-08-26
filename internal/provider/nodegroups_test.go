package provider

import (
	"context"
	"testing"

	. "github.com/onsi/gomega"
	serverscom "github.com/serverscom/serverscom-go-client/pkg"
	gomock "go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/serverscom/serverscom-k8s-autoscaler-provider/internal/protos"
	serverscom_testing "github.com/serverscom/serverscom-k8s-autoscaler-provider/internal/testing"
)

// expectNodeGroups wires one paginated listing of the autoscale groups of the cluster.
func expectNodeGroups(t *testing.T, api *serverscom_testing.MockKubernetesClustersService, groups []serverscom.KubernetesClusterAutoscaleNodeGroup) {
	t.Helper()

	ctrl := gomock.NewController(t)
	collection := serverscom_testing.NewMockCollection[serverscom.KubernetesClusterAutoscaleNodeGroup](ctrl)

	collection.EXPECT().SetPerPage(perPage).Return(collection)
	collection.EXPECT().Collect(gomock.Any()).Return(groups, nil)

	api.EXPECT().AutoscaleNodeGroups(testClusterID).Return(collection)
}

func TestNodeGroups(t *testing.T) {
	g := NewGomegaWithT(t)

	p, api := newTestProvider(t)
	expectNodeGroups(t, api, []serverscom.KubernetesClusterAutoscaleNodeGroup{
		{ID: "group1", Name: "ng-1", MinNodes: 1, MaxNodes: 5, TargetNodes: 2, CurrentNodes: 2},
		{ID: "group2", Name: "ng-2", MinNodes: 0, MaxNodes: 3, TargetNodes: 0, CurrentNodes: 0},
	})

	res, err := p.NodeGroups(context.Background(), &protos.NodeGroupsRequest{})

	g.Expect(err).To(BeNil())
	g.Expect(res.GetNodeGroups()).To(HaveLen(2))

	g.Expect(res.GetNodeGroups()[0].GetId()).To(Equal("group1"))
	g.Expect(res.GetNodeGroups()[0].GetMinSize()).To(Equal(int32(1)))
	g.Expect(res.GetNodeGroups()[0].GetMaxSize()).To(Equal(int32(5)))
	g.Expect(res.GetNodeGroups()[0].GetDebug()).To(ContainSubstring("min=1 max=5 target=2 current=2"))

	g.Expect(res.GetNodeGroups()[1].GetMinSize()).To(Equal(int32(0)))
	g.Expect(res.GetNodeGroups()[1].GetMaxSize()).To(Equal(int32(3)))
}

// Nothing is cached, so a bound edited through the portal shows up as soon as the autoscaler
// asks again - which it does after Refresh drops its own copy.
func TestNodeGroupsServesCurrentBounds(t *testing.T) {
	g := NewGomegaWithT(t)

	p, api := newTestProvider(t)

	expectNodeGroups(t, api, []serverscom.KubernetesClusterAutoscaleNodeGroup{
		{ID: "group1", MinNodes: 1, MaxNodes: 5},
	})
	expectNodeGroups(t, api, []serverscom.KubernetesClusterAutoscaleNodeGroup{
		{ID: "group1", MinNodes: 2, MaxNodes: 9},
	})

	first, err := p.NodeGroups(context.Background(), &protos.NodeGroupsRequest{})
	g.Expect(err).To(BeNil())

	_, err = p.Refresh(context.Background(), &protos.RefreshRequest{})
	g.Expect(err).To(BeNil())

	second, err := p.NodeGroups(context.Background(), &protos.NodeGroupsRequest{})
	g.Expect(err).To(BeNil())

	g.Expect(first.GetNodeGroups()[0].GetMaxSize()).To(Equal(int32(5)))
	g.Expect(second.GetNodeGroups()[0].GetMinSize()).To(Equal(int32(2)))
	g.Expect(second.GetNodeGroups()[0].GetMaxSize()).To(Equal(int32(9)))
}

func TestNodeGroupForNode(t *testing.T) {
	g := NewGomegaWithT(t)

	p, api := newTestProvider(t)

	api.EXPECT().
		GetNode(gomock.Any(), testClusterID, "node1").
		Return(&serverscom.KubernetesClusterNode{
			ID:        "node1",
			Status:    "active",
			NodeGroup: serverscom.KubernetesClusterNodeGroupInfo{ID: testGroupID, Type: autoscaleGroupType},
		}, nil)

	api.EXPECT().
		GetAutoscaleNodeGroup(gomock.Any(), testClusterID, testGroupID).
		Return(testGroup(1, 5, 2, 2), nil)

	res, err := p.NodeGroupForNode(context.Background(), &protos.NodeGroupForNodeRequest{
		Node: &protos.ExternalGrpcNode{Name: "node-1", ProviderID: buildProviderID(testClusterID, "node1")},
	})

	g.Expect(err).To(BeNil())
	g.Expect(res.GetNodeGroup().GetId()).To(Equal(testGroupID))
	g.Expect(res.GetNodeGroup().GetMinSize()).To(Equal(int32(1)))
	g.Expect(res.GetNodeGroup().GetMaxSize()).To(Equal(int32(5)))
}

// An empty node group id means "not the autoscaler's business". Masters, nodes of static
// groups and nodes of other clouds all take that path, and none of them is an error.
func TestNodeGroupForNodeUnmanaged(t *testing.T) {
	cases := []struct {
		name       string
		providerID string
		// setup wires the API calls the case is expected to make, if any.
		setup func(api *serverscom_testing.MockKubernetesClustersService)
	}{
		{name: "no provider id", providerID: ""},
		{name: "another cloud", providerID: "aws:///eu-west-1a/i-0123"},
		{name: "baremetal node", providerID: "serverscom://kubernetes-baremetal-node/node1"},
		{name: "another cluster", providerID: buildProviderID("otherCluster", "node1")},
		{
			name:       "static node group",
			providerID: buildProviderID(testClusterID, "node1"),
			setup: func(api *serverscom_testing.MockKubernetesClustersService) {
				api.EXPECT().GetNode(gomock.Any(), testClusterID, "node1").Return(&serverscom.KubernetesClusterNode{
					ID:        "node1",
					Status:    "active",
					NodeGroup: serverscom.KubernetesClusterNodeGroupInfo{ID: "static1", Type: "static"},
				}, nil)
			},
		},
		{
			name:       "released node",
			providerID: buildProviderID(testClusterID, "node1"),
			setup: func(api *serverscom_testing.MockKubernetesClustersService) {
				api.EXPECT().GetNode(gomock.Any(), testClusterID, "node1").Return(&serverscom.KubernetesClusterNode{
					ID:        "node1",
					Status:    "removed",
					NodeGroup: serverscom.KubernetesClusterNodeGroupInfo{ID: testGroupID, Type: autoscaleGroupType},
				}, nil)
			},
		},
		{
			name:       "node gone from the api",
			providerID: buildProviderID(testClusterID, "node1"),
			setup: func(api *serverscom_testing.MockKubernetesClustersService) {
				api.EXPECT().GetNode(gomock.Any(), testClusterID, "node1").
					Return(nil, &serverscom.NotFoundError{StatusCode: 404, ErrorCode: "NOT_FOUND", Message: "Not found"})
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewGomegaWithT(t)

			p, api := newTestProvider(t)
			if tc.setup != nil {
				tc.setup(api)
			}

			api.EXPECT().GetAutoscaleNodeGroup(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)

			res, err := p.NodeGroupForNode(context.Background(), &protos.NodeGroupForNodeRequest{
				Node: &protos.ExternalGrpcNode{Name: "node-1", ProviderID: tc.providerID},
			})

			g.Expect(err).To(BeNil())
			g.Expect(res.GetNodeGroup().GetId()).To(BeEmpty())
		})
	}
}

func TestNodeGroupTargetSize(t *testing.T) {
	g := NewGomegaWithT(t)

	p, api := newTestProvider(t)

	api.EXPECT().
		GetAutoscaleNodeGroup(gomock.Any(), testClusterID, testGroupID).
		Return(testGroup(1, 5, 4, 2), nil)

	res, err := p.NodeGroupTargetSize(context.Background(), &protos.NodeGroupTargetSizeRequest{Id: testGroupID})

	g.Expect(err).To(BeNil())
	g.Expect(res.GetTargetSize()).To(Equal(int32(4)))
}

func TestNodeGroupRPCsRequireAnID(t *testing.T) {
	g := NewGomegaWithT(t)

	p, _ := newTestProvider(t)
	ctx := context.Background()

	_, err := p.NodeGroupTargetSize(ctx, &protos.NodeGroupTargetSizeRequest{})
	g.Expect(status.Code(err)).To(Equal(codes.InvalidArgument))

	_, err = p.NodeGroupNodes(ctx, &protos.NodeGroupNodesRequest{})
	g.Expect(status.Code(err)).To(Equal(codes.InvalidArgument))

	_, err = p.NodeGroupTemplateNodeInfo(ctx, &protos.NodeGroupTemplateNodeInfoRequest{})
	g.Expect(status.Code(err)).To(Equal(codes.InvalidArgument))

	_, err = p.NodeGroupIncreaseSize(ctx, &protos.NodeGroupIncreaseSizeRequest{Delta: 1})
	g.Expect(status.Code(err)).To(Equal(codes.InvalidArgument))

	_, err = p.NodeGroupForNode(ctx, &protos.NodeGroupForNodeRequest{})
	g.Expect(status.Code(err)).To(Equal(codes.InvalidArgument))
}
