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

// The provider never repeats a call to the API on its own. Increasing the size is not
// idempotent - it shifts the target size by a delta - so a retry would order the hardware
// twice; the same reasoning holds for the other mutating calls. Every mutating RPC is checked
// here against a 5xx, the failure mode most likely to tempt a retry.
func TestMutatingRPCsCallTheAPIExactlyOnce(t *testing.T) {
	apiErr := func() error {
		return &serverscom.InternalServerError{StatusCode: 500, ErrorCode: "INTERNAL", Message: "boom"}
	}

	cases := []struct {
		name string
		// expect wires the single call the RPC is allowed to make.
		expect func(api *serverscom_testing.MockKubernetesClustersService)
		call   func(p *Provider) error
	}{
		{
			name: "increase size",
			expect: func(api *serverscom_testing.MockKubernetesClustersService) {
				api.EXPECT().
					IncreaseAutoscaleNodeGroupSize(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil, apiErr()).
					Times(1)
			},
			call: func(p *Provider) error {
				_, err := p.NodeGroupIncreaseSize(context.Background(),
					&protos.NodeGroupIncreaseSizeRequest{Id: testGroupID, Delta: 1})
				return err
			},
		},
		{
			name: "decrease target size",
			expect: func(api *serverscom_testing.MockKubernetesClustersService) {
				api.EXPECT().
					DecreaseAutoscaleNodeGroupTargetSize(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil, apiErr()).
					Times(1)
			},
			call: func(p *Provider) error {
				_, err := p.NodeGroupDecreaseTargetSize(context.Background(),
					&protos.NodeGroupDecreaseTargetSizeRequest{Id: testGroupID, Delta: -1})
				return err
			},
		},
		{
			name: "delete nodes",
			expect: func(api *serverscom_testing.MockKubernetesClustersService) {
				api.EXPECT().
					DeleteAutoscaleNodeGroupNodes(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil, apiErr()).
					Times(1)
			},
			call: func(p *Provider) error {
				_, err := p.NodeGroupDeleteNodes(context.Background(), &protos.NodeGroupDeleteNodesRequest{
					Id:    testGroupID,
					Nodes: []*protos.ExternalGrpcNode{{ProviderID: buildProviderID(testClusterID, "node1")}},
				})
				return err
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewGomegaWithT(t)

			p, api := newTestProvider(t)
			tc.expect(api)

			// gomock fails the test if the call is made more than the expected number of times.
			g.Expect(tc.call(p)).To(HaveOccurred())
		})
	}
}
