# cmd/collector-kubernetes-live

The Kubernetes live collector binary (`eshu-collector-kubernetes-live`). It is
the foundation toward issue #388; correlation and drift are reducer-owned and
out of scope here.

## What it does

- Loads configured clusters from environment, builds a read-only client-go
  adapter per cluster (kubeconfig or in-cluster auth), and lists a fixed core
  resource set.
- Maps objects into `kubernetes_live.pod_template`,
  `kubernetes_live.relationship`, and `kubernetes_live.warning` facts and commits
  them through `collector.Service` and `postgres.NewIngestionStore`.
- Emits Kubernetes secrets/IAM posture facts for ServiceAccount, RBAC, workload
  identity use, IRSA annotations, and GKE Workload Identity annotations when a
  cluster entry declares its workload pool.
- Hosts the shared admin surface (`/healthz`, `/readyz`, `/admin/status`,
  `/metrics`) and optional pprof.

It is read-only and metadata-only: it never mutates the cluster and never reads
Secret values, ConfigMap data payloads, environment variable values, or logs.

## Configuration

| Env var | Required | Purpose |
| --- | --- | --- |
| `ESHU_KUBERNETES_LIVE_COLLECTOR_INSTANCE_ID` | yes | Durable collector instance id stamped on facts. |
| `ESHU_KUBERNETES_LIVE_CLUSTERS_JSON` | yes | JSON object with a `clusters` array (see below). |
| `ESHU_KUBERNETES_LIVE_POLL_INTERVAL` | no (default `5m`) | Delay between snapshot passes. |

Each cluster entry:

```json
{
  "clusters": [
    {
      "cluster_id": "prod-us-east-1",
      "display_name": "prod us-east-1",
      "provider": "gke",
      "gcp_workload_pool": "my-project.svc.id.goog",
      "environment": "production",
      "fencing_token": 1,
      "auth_mode": "kubeconfig",
      "kubeconfig_path": "/etc/eshu/kubeconfig",
      "kube_context": "prod",
      "qps": 20,
      "burst": 30
    },
    {
      "cluster_id": "in-cluster",
      "auth_mode": "in_cluster"
    }
  ]
}
```

`cluster_id` is the operator-declared durable cluster identity and the scope
anchor; the collector never infers identity from the API server URL. `auth_mode`
is `kubeconfig` (requires `kubeconfig_path`, optional `kube_context`) or
`in_cluster` (mounted service-account token). `gcp_workload_pool` is optional
and should be set for GKE clusters whose ServiceAccounts use the
`iam.gke.io/gcp-service-account` annotation; without it the annotation is not
emitted as GCP Workload Identity evidence.

## RBAC

Grant only `get`, `list`, and `watch` on:

- core (`""`): namespaces, pods, services, serviceaccounts
- `apps`: deployments, replicasets, statefulsets, daemonsets
- `batch`: jobs, cronjobs
- `networking.k8s.io`: ingresses
- `rbac.authorization.k8s.io`: roles, clusterroles, rolebindings,
  clusterrolebindings

Exclude Secret values. Prefer a namespace `RoleBinding` when namespace-scoped
collection is enough (ClusterRoles and ClusterRoleBindings still require
cluster-scoped read access to list).

## Telemetry

Metrics use the `eshu_dp_kubernetes_` prefix; labels never include namespace or
object names. Spans: `kubernetes_live.snapshot`, `kubernetes_live.api_call`.

## Deferred

Claim-driven collection, watch mode, additional resource kinds, and the #388
reducer/read-model are follow-up work.
