package provider

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"
	klog "k8s.io/klog/v2"

	"github.com/serverscom/serverscom-k8s-autoscaler-provider/internal/protos"
)

// GPULabel returns an empty label: GPU nodes are out of scope.
func (p *Provider) GPULabel(context.Context, *protos.GPULabelRequest) (*protos.GPULabelResponse, error) {
	return &protos.GPULabelResponse{}, nil
}

// GetAvailableGPUTypes returns an empty map: GPU nodes are out of scope.
func (p *Provider) GetAvailableGPUTypes(context.Context, *protos.GetAvailableGPUTypesRequest) (*protos.GetAvailableGPUTypesResponse, error) {
	return &protos.GetAvailableGPUTypesResponse{GpuTypes: map[string]*anypb.Any{}}, nil
}

// Refresh is called before every autoscaler loop and is where a provider would drop its caches.
//
// This one has none: every RPC reads the Public API afresh. That is what makes a min/max edited
// through the portal visible as soon as the autoscaler drops its own cached copy, and what
// makes a restart or a switch to another provider instance a non-event.
func (p *Provider) Refresh(context.Context, *protos.RefreshRequest) (*protos.RefreshResponse, error) {
	klog.V(5).Info("Refresh: nothing to drop, the provider holds no state")
	return &protos.RefreshResponse{}, nil
}

// Cleanup releases resources before the provider is destroyed. There are none.
func (p *Provider) Cleanup(context.Context, *protos.CleanupRequest) (*protos.CleanupResponse, error) {
	klog.V(5).Info("Cleanup: nothing to release, the provider holds no state")
	return &protos.CleanupResponse{}, nil
}

// PricingNodePrice is not implemented: the Public API contract carries no prices, and without
// them the price expander does not work anyway.
func (p *Provider) PricingNodePrice(context.Context, *protos.PricingNodePriceRequest) (*protos.PricingNodePriceResponse, error) {
	return nil, status.Error(codes.Unimplemented, "the servers.com provider reports no prices")
}

// PricingPodPrice is not implemented, see PricingNodePrice.
func (p *Provider) PricingPodPrice(context.Context, *protos.PricingPodPriceRequest) (*protos.PricingPodPriceResponse, error) {
	return nil, status.Error(codes.Unimplemented, "the servers.com provider reports no prices")
}

// NodeGroupGetOptions is not implemented: scale-down thresholds and timeouts are set per
// cluster through the autoscaler's own flags, not per node group.
func (p *Provider) NodeGroupGetOptions(context.Context, *protos.NodeGroupAutoscalingOptionsRequest) (*protos.NodeGroupAutoscalingOptionsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "the servers.com provider has no per-node-group autoscaling options")
}
