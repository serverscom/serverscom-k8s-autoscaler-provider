# serverscom-k8s-autoscaler-provider

An [external gRPC cloud provider](https://github.com/kubernetes-sigs/cluster-autoscaler/tree/562c02c17afedc1a1699b6c772018c009e790e41/pkg/cloudprovider/externalgrpc)
for the Kubernetes cluster-autoscaler, backed by the Servers.com Public API.

It runs as a separate service next to an **upstream, unforked** cluster-autoscaler started with
`--cloud-provider=externalgrpc`, and moves the autoscale node groups of one Kubernetes cluster
between the bounds their owner set through the API or the portal.

## Design

**Stateless.** Nothing is cached between calls, there is no coordination between instances, the
Public API is the only source of truth. Several provider instances may run at once while exactly
one autoscaler is active; a restart or a switch to another instance changes nothing. It also
means `Refresh` has nothing to drop: a min/max edited through the portal reaches the autoscaler
as soon as it drops its own cached copy.

**No retries.** Every API call is made exactly once. On a timeout, a broken connection or a 5xx
the error goes up to the autoscaler, which decides what to do. Increasing the size of a group is
not idempotent - it shifts the target size by a delta - so retrying here would order the
hardware twice.

## RPCs

| RPC | Public API |
|---|---|
| `NodeGroups` | `GET  /kubernetes_clusters/{id}/autoscale_node_groups` |
| `NodeGroupForNode` | `GET  .../nodes/{node_id}` then `GET .../autoscale_node_groups/{id}` |
| `NodeGroupTargetSize` | `GET  .../autoscale_node_groups/{id}` -> `target_nodes` |
| `NodeGroupIncreaseSize` | `POST .../increase_size` |
| `NodeGroupDecreaseTargetSize` | `POST .../decrease_target_size` |
| `NodeGroupDeleteNodes` | `POST .../delete_nodes` |
| `NodeGroupNodes` | `GET  .../autoscale_node_groups/{id}/nodes` |
| `NodeGroupTemplateNodeInfo` | `GET  .../autoscale_node_groups/{id}/autoscale_template` |
| `Refresh`, `Cleanup` | none, the provider holds no state |
| `GPULabel`, `GetAvailableGPUTypes` | none, empty values |
| `PricingNodePrice`, `PricingPodPrice`, `NodeGroupGetOptions` | reported as `Unimplemented` |

The autoscaler passes a **negative** delta to `NodeGroupDecreaseTargetSize` while
`decrease_target_size` takes a positive one; the sign is converted explicitly and a
non-negative delta is rejected before any API call is made.

### Node states

| API status | gRPC state | meaning |
|---|---|---|
| `pending` | `instanceCreating` | the order is accepted, no hardware handed out yet |
| `processing` | `instanceCreating` | provisioning is under way |
| `created` | `instanceCreating` | built, but has not joined the cluster yet |
| `active` | `instanceRunning` | in the cluster |
| `upgrading` | `instanceRunning` | in the cluster, an upgrade is running |
| `removed` | not reported at all | |

`created` is a *creating* instance on purpose: the API refuses to delete such a node with a
conflict until it has joined. Released nodes are filtered out because the API keeps returning
them indefinitely - reporting one would leave the autoscaler staring at an instance that is
eternally being deleted. That filtering also lines the list up with the group's own node
counter, which likewise counts everything but `removed`.

### Node template

`NodeGroupTemplateNodeInfo` returns a serialized `v1.Node` carrying capacity only: cpu from
`logical_cpu_count`, memory from `ram_size` and the pod capacity from `--max-pods`. Allocatable
mirrors capacity, because the scale-up simulation bin-packs against allocatable.

There are deliberately **no labels and no taints** in the template. The consequence is accepted:
pods that pick nodes through `nodeSelector` or affinity on labels will not trigger a scale-up of
such a group.

### Errors

API errors reach the autoscaler as gRPC errors with the API error code preserved in the message.
Conflicts - the group is at its maximum or minimum, the target size would drop below the nodes
the group already has, a node has not joined the cluster yet, the account's SBM limits are
exceeded - come back as `FAILED_PRECONDITION` and are never turned into a successful response.

## Running it

```
SERVERSCOM_TOKEN=... SERVERSCOM_CLUSTER_ID=... \
  serverscom-k8s-autoscaler-provider --address=:8086 --v=1
```

| Flag | Default | |
|---|---|---|
| `--address` | `:8086` | address to expose the gRPC service on |
| `--cluster-id` | `$SERVERSCOM_CLUSTER_ID` | the cluster to serve, required |
| `--max-pods` | `110` | pod capacity in the node template |
| `--api-timeout` | `30s` | applied only when the incoming call carries no deadline |
| `--v` | `0` | klog verbosity; `5` logs every call |

| Environment | |
|---|---|
| `SERVERSCOM_TOKEN` | API token, required |
| `SERVERSCOM_BASE_URL` | API endpoint, defaults to `https://api.servers.com/v1` |
| `SERVERSCOM_CLUSTER_ID` | default for `--cluster-id` |

One instance serves exactly one cluster.

The listener is plaintext. Anyone who can reach it can trigger the creation and deletion of real
hardware, so it must not be exposed beyond the autoscaler - keep it on a private network or
behind a network policy that admits only the autoscaler pod.

### Autoscaler side

`--cloud-provider=externalgrpc --cloud-config=/etc/cluster-autoscaler/cloud-config.yaml` with:

```yaml
address: "provider-host:8086"
grpc_timeout: 10s
```

`grpc_timeout` arrives here as the deadline of every call and takes precedence over
`--api-timeout`.

Example manifests, and a walkthrough for running the whole thing against a stub API in a local
cluster, are in [deploy/](deploy/README.md).

## Development

```
make generate   # regenerates the gRPC bindings and the mocks
make build
make test
make vet
```

`make generate` needs `protoc`, `protoc-gen-go`, `protoc-gen-go-grpc` and `mockgen` on `PATH`.
The contract this provider is written against is pinned and vendored, see [proto/README.md](proto/README.md).

## Not covered

Creating and deleting node groups (the autoscaler only moves existing ones within their bounds),
changing the location or flavour of an existing group, labels and taints in the node template,
reporting provisioning failures, the price expander, GPU support, per-node-group autoscaling
options, retries or compensating logic, transport security, health checking, static node groups,
and serving several clusters from one instance.