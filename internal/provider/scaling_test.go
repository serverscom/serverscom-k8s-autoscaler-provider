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
)

// TestNodeGroupDecreaseTargetSize covers the sign conversion: the autoscaler asks to lower the
// target with a negative delta, the API endpoint takes a positive one.
func TestNodeGroupDecreaseTargetSize(t *testing.T) {
	cases := []struct {
		name      string
		delta     int32
		wantDelta int64
	}{
		{name: "minus one", delta: -1, wantDelta: 1},
		{name: "minus two", delta: -2, wantDelta: 2},
		{name: "minus ten", delta: -10, wantDelta: 10},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewGomegaWithT(t)

			p, api := newTestProvider(t)

			api.EXPECT().
				DecreaseAutoscaleNodeGroupTargetSize(
					gomock.Any(), testClusterID, testGroupID,
					serverscom.KubernetesClusterAutoscaleNodeGroupDecreaseTargetSizeInput{Delta: tc.wantDelta},
				).
				Return(testGroup(1, 5, 1, 1), nil).
				Times(1)

			res, err := p.NodeGroupDecreaseTargetSize(context.Background(),
				&protos.NodeGroupDecreaseTargetSizeRequest{Id: testGroupID, Delta: tc.delta})

			g.Expect(err).To(BeNil())
			g.Expect(res).NotTo(BeNil())
		})
	}
}

// A non-negative delta is rejected before the API is touched: reading it the wrong way round
// would raise the target size instead of lowering it.
func TestNodeGroupDecreaseTargetSizeRejectsNonNegativeDelta(t *testing.T) {
	for _, delta := range []int32{0, 1, 7} {
		g := NewGomegaWithT(t)

		p, api := newTestProvider(t)

		api.EXPECT().
			DecreaseAutoscaleNodeGroupTargetSize(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Times(0)

		_, err := p.NodeGroupDecreaseTargetSize(context.Background(),
			&protos.NodeGroupDecreaseTargetSizeRequest{Id: testGroupID, Delta: delta})

		g.Expect(status.Code(err)).To(Equal(codes.InvalidArgument))
	}
}

func TestNodeGroupIncreaseSize(t *testing.T) {
	g := NewGomegaWithT(t)

	p, api := newTestProvider(t)

	api.EXPECT().
		IncreaseAutoscaleNodeGroupSize(
			gomock.Any(), testClusterID, testGroupID,
			serverscom.KubernetesClusterAutoscaleNodeGroupIncreaseSizeInput{Delta: 2},
		).
		Return(testGroup(1, 5, 4, 2), nil).
		Times(1)

	_, err := p.NodeGroupIncreaseSize(context.Background(),
		&protos.NodeGroupIncreaseSizeRequest{Id: testGroupID, Delta: 2})

	g.Expect(err).To(BeNil())
}

func TestNodeGroupIncreaseSizeRejectsNonPositiveDelta(t *testing.T) {
	for _, delta := range []int32{0, -1} {
		g := NewGomegaWithT(t)

		p, api := newTestProvider(t)

		api.EXPECT().
			IncreaseAutoscaleNodeGroupSize(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Times(0)

		_, err := p.NodeGroupIncreaseSize(context.Background(),
			&protos.NodeGroupIncreaseSizeRequest{Id: testGroupID, Delta: delta})

		g.Expect(status.Code(err)).To(Equal(codes.InvalidArgument))
	}
}

// Reaching the maximum size is a conflict on the API side and has to surface as an error with
// the API code intact, never as a successful response.
func TestNodeGroupIncreaseSizeSurfacesConflict(t *testing.T) {
	g := NewGomegaWithT(t)

	p, api := newTestProvider(t)

	apiErr := &serverscom.ConflictError{
		StatusCode: 409,
		ErrorCode:  "MAXIMUM_NODES_REACHED",
		Message:    "This group has reached its maximum size",
	}

	api.EXPECT().
		IncreaseAutoscaleNodeGroupSize(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, apiErr).
		Times(1)

	_, err := p.NodeGroupIncreaseSize(context.Background(),
		&protos.NodeGroupIncreaseSizeRequest{Id: testGroupID, Delta: 1})

	g.Expect(status.Code(err)).To(Equal(codes.FailedPrecondition))
	g.Expect(err.Error()).To(ContainSubstring("MAXIMUM_NODES_REACHED"))
}

func TestNodeGroupDeleteNodes(t *testing.T) {
	g := NewGomegaWithT(t)

	p, api := newTestProvider(t)

	// All the nodes go out in one request so that the target size moves once.
	api.EXPECT().
		DeleteAutoscaleNodeGroupNodes(
			gomock.Any(), testClusterID, testGroupID,
			serverscom.KubernetesClusterAutoscaleNodeGroupDeleteNodesInput{NodeIDs: []string{"node1", "node2"}},
		).
		Return(testGroup(0, 5, 1, 1), nil).
		Times(1)

	_, err := p.NodeGroupDeleteNodes(context.Background(), &protos.NodeGroupDeleteNodesRequest{
		Id: testGroupID,
		Nodes: []*protos.ExternalGrpcNode{
			{Name: "node-1", ProviderID: buildProviderID(testClusterID, "node1")},
			{Name: "node-2", ProviderID: buildProviderID(testClusterID, "node2")},
		},
	})

	g.Expect(err).To(BeNil())
}

func TestNodeGroupDeleteNodesRejectsForeignNodes(t *testing.T) {
	cases := []struct {
		name       string
		providerID string
	}{
		{name: "another cluster", providerID: buildProviderID("otherCluster", "node1")},
		{name: "not an autoscale node", providerID: "serverscom://kubernetes-baremetal-node/node1"},
		{name: "empty", providerID: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewGomegaWithT(t)

			p, api := newTestProvider(t)

			api.EXPECT().
				DeleteAutoscaleNodeGroupNodes(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Times(0)

			_, err := p.NodeGroupDeleteNodes(context.Background(), &protos.NodeGroupDeleteNodesRequest{
				Id:    testGroupID,
				Nodes: []*protos.ExternalGrpcNode{{Name: "node-1", ProviderID: tc.providerID}},
			})

			g.Expect(status.Code(err)).To(Equal(codes.InvalidArgument))
		})
	}
}

// A node that has not joined the cluster yet cannot be released; the conflict reaches the
// autoscaler untouched.
func TestNodeGroupDeleteNodesSurfacesConflict(t *testing.T) {
	g := NewGomegaWithT(t)

	p, api := newTestProvider(t)

	api.EXPECT().
		DeleteAutoscaleNodeGroupNodes(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, &serverscom.ConflictError{
			StatusCode: 409,
			ErrorCode:  "K8S_NODES_NOT_READY",
			Message:    "A node can only be deleted once it has joined the cluster",
		}).
		Times(1)

	_, err := p.NodeGroupDeleteNodes(context.Background(), &protos.NodeGroupDeleteNodesRequest{
		Id:    testGroupID,
		Nodes: []*protos.ExternalGrpcNode{{Name: "node-1", ProviderID: buildProviderID(testClusterID, "node1")}},
	})

	g.Expect(status.Code(err)).To(Equal(codes.FailedPrecondition))
	g.Expect(err.Error()).To(ContainSubstring("K8S_NODES_NOT_READY"))
}
