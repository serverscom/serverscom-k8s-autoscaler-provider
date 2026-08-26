# proto

`externalgrpc.proto` is a verbatim copy of the upstream cluster-autoscaler external gRPC
cloud provider contract, pinned to:

    kubernetes-sigs/cluster-autoscaler
    commit 562c02c17afedc1a1699b6c772018c009e790e41
    pkg/cloudprovider/externalgrpc/protos/externalgrpc.proto

The Go bindings under `internal/protos` are generated from this file by `make generate`. 

To move to a newer contract version, re-download the file at the new commit, update the pin
above, and re-run `make generate`.
