package provider

import (
	"context"
	"testing"

	. "github.com/onsi/gomega"
	serverscom "github.com/serverscom/serverscom-go-client/pkg"
	gomock "go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	apiv1 "k8s.io/api/core/v1"

	"github.com/serverscom/serverscom-k8s-autoscaler-provider/internal/protos"
)

func TestNodeGroupTemplateNodeInfo(t *testing.T) {
	g := NewGomegaWithT(t)

	p, api := newTestProvider(t)

	cpu := int64(16)
	ram := int64(32)

	api.EXPECT().
		GetAutoscaleNodeGroupTemplate(gomock.Any(), testClusterID, testGroupID).
		Return(&serverscom.KubernetesClusterAutoscaleNodeGroupTemplate{
			FlavorName:      "P-101",
			LogicalCPUCount: &cpu,
			RAMSize:         &ram,
		}, nil)

	res, err := p.NodeGroupTemplateNodeInfo(context.Background(),
		&protos.NodeGroupTemplateNodeInfoRequest{Id: testGroupID})

	g.Expect(err).To(BeNil())

	node := &apiv1.Node{}
	g.Expect(node.Unmarshal(res.GetNodeBytes())).To(Succeed())

	g.Expect(node.Status.Capacity.Cpu().Value()).To(Equal(int64(16)))
	g.Expect(node.Status.Capacity.Memory().Value()).To(Equal(int64(32) * bytesPerGB))
	g.Expect(node.Status.Capacity.Pods().Value()).To(Equal(int64(testMaxPods)))

	// The scale-up simulation bin-packs against allocatable, so it mirrors capacity.
	g.Expect(node.Status.Allocatable.Cpu().Value()).To(Equal(int64(16)))
	g.Expect(node.Status.Allocatable.Memory().Value()).To(Equal(int64(32) * bytesPerGB))
	g.Expect(node.Status.Allocatable.Pods().Value()).To(Equal(int64(testMaxPods)))

	// Capacity only: no labels, no taints, nothing else.
	g.Expect(node.Labels).To(BeEmpty())
	g.Expect(node.Spec.Taints).To(BeEmpty())
	g.Expect(node.Status.Conditions).To(BeEmpty())
}

// A group whose hardware shape the API does not know yet is an error, not Unimplemented: the
// autoscaler caches Unimplemented for the whole lifetime of the node group object.
func TestNodeGroupTemplateNodeInfoWithoutShape(t *testing.T) {
	cases := []struct {
		name     string
		template *serverscom.KubernetesClusterAutoscaleNodeGroupTemplate
	}{
		{name: "no cpu", template: &serverscom.KubernetesClusterAutoscaleNodeGroupTemplate{FlavorName: "P-101", RAMSize: ptr(int64(32))}},
		{name: "no ram", template: &serverscom.KubernetesClusterAutoscaleNodeGroupTemplate{FlavorName: "P-101", LogicalCPUCount: ptr(int64(16))}},
		{name: "neither", template: &serverscom.KubernetesClusterAutoscaleNodeGroupTemplate{FlavorName: "P-101"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewGomegaWithT(t)

			p, api := newTestProvider(t)

			api.EXPECT().
				GetAutoscaleNodeGroupTemplate(gomock.Any(), testClusterID, testGroupID).
				Return(tc.template, nil)

			_, err := p.NodeGroupTemplateNodeInfo(context.Background(),
				&protos.NodeGroupTemplateNodeInfoRequest{Id: testGroupID})

			g.Expect(status.Code(err)).To(Equal(codes.FailedPrecondition))
		})
	}
}

func ptr[T any](v T) *T {
	return &v
}
