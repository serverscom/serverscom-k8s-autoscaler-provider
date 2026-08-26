MODULE := github.com/serverscom/serverscom-k8s-autoscaler-provider
PROTO_PKG := $(MODULE)/internal/protos
GO_CLIENT_PATH := ./vendor/github.com/serverscom/serverscom-go-client/pkg
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

.PHONY: build test generate proto mocks

build:
	go build -ldflags "$(LDFLAGS)" -o build/serverscom-k8s-autoscaler-provider .

test:
	go test ./... -count=1 -coverprofile cp.out

generate: proto mocks

# Regenerates the gRPC bindings from the pinned upstream contract, see proto/README.md.
proto:
	protoc -I proto \
	  --go_out=. --go_opt=module=$(MODULE) --go_opt=Mexternalgrpc.proto=$(PROTO_PKG) \
	  --go-grpc_out=. --go-grpc_opt=module=$(MODULE) --go-grpc_opt=Mexternalgrpc.proto=$(PROTO_PKG) \
	  proto/externalgrpc.proto

# The mocks are generated from the vendored go-client, so the import paths they come out with
# have to have the vendor prefix stripped, same as the cloud-controller-manager does.
mocks:
	go mod tidy
	go mod vendor
	mockgen --destination ./internal/testing/kubernetes_clusters_mock.go --package=serverscom_testing --source $(GO_CLIENT_PATH)/kubernetes_clusters.go
	mockgen --destination ./internal/testing/collection_mock.go --package=serverscom_testing --source $(GO_CLIENT_PATH)/collection.go
	sed -i '' 's|$(MODULE)/vendor/||' \
	  ./internal/testing/kubernetes_clusters_mock.go \
	  ./internal/testing/collection_mock.go
