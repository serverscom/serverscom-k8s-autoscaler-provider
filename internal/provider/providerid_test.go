package provider

import (
	"testing"

	. "github.com/onsi/gomega"
)

func TestBuildProviderID(t *testing.T) {
	g := NewGomegaWithT(t)

	g.Expect(buildProviderID("MYerYkdO", "ELe36rd6")).
		To(Equal("serverscom://kubernetes-autoscale-node/MYerYkdO/ELe36rd6"))
}

func TestParseAutoscaleProviderID(t *testing.T) {
	cases := []struct {
		name       string
		providerID string
		clusterID  string
		nodeID     string
		wantErr    bool
	}{
		{
			name:       "autoscale node",
			providerID: "serverscom://kubernetes-autoscale-node/MYerYkdO/ELe36rd6",
			clusterID:  "MYerYkdO",
			nodeID:     "ELe36rd6",
		},
		{
			name:       "underscored node type",
			providerID: "serverscom://kubernetes_autoscale_node/MYerYkdO/ELe36rd6",
			clusterID:  "MYerYkdO",
			nodeID:     "ELe36rd6",
		},
		{name: "empty", providerID: "", wantErr: true},
		{name: "another cloud", providerID: "aws:///eu-west-1a/i-0123", wantErr: true},
		{name: "baremetal node", providerID: "serverscom://kubernetes-baremetal-node/a", wantErr: true},
		{name: "cloud instance", providerID: "serverscom://cloud-instance/a", wantErr: true},
		{name: "missing cluster id", providerID: "serverscom://kubernetes-autoscale-node/ELe36rd6", wantErr: true},
		{name: "too many segments", providerID: "serverscom://kubernetes-autoscale-node/a/b/c", wantErr: true},
		{name: "empty node id", providerID: "serverscom://kubernetes-autoscale-node/a/", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewGomegaWithT(t)

			clusterID, nodeID, err := parseAutoscaleProviderID(tc.providerID)

			if tc.wantErr {
				g.Expect(err).To(HaveOccurred())
				return
			}

			g.Expect(err).To(BeNil())
			g.Expect(clusterID).To(Equal(tc.clusterID))
			g.Expect(nodeID).To(Equal(tc.nodeID))
		})
	}
}

func TestProviderIDRoundTrip(t *testing.T) {
	g := NewGomegaWithT(t)

	clusterID, nodeID, err := parseAutoscaleProviderID(buildProviderID("cluster1", "node1"))

	g.Expect(err).To(BeNil())
	g.Expect(clusterID).To(Equal("cluster1"))
	g.Expect(nodeID).To(Equal("node1"))
}
