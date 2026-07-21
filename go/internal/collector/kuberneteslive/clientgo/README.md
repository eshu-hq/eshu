# internal/collector/kuberneteslive/clientgo

The client-go adapter for the Kubernetes live collector. This is the only place
that imports client-go and the Kubernetes typed API, keeping the collector
source (`internal/collector/kuberneteslive`) backend-neutral and unit-testable
with fakes.

## Responsibilities

- Build a read-only `*rest.Config` and typed clientset from one of two auth
  modes:
  - `in_cluster` — the mounted service-account token and in-cluster API address.
  - `kubeconfig` — a kubeconfig file path plus an optional context name.
- Implement `kuberneteslive.Client` by listing the core resource set with
  bounded pagination (continue tokens), mapping typed objects into the
  collector's neutral metadata views.
- List ServiceAccounts, Roles, ClusterRoles, RoleBindings, and
  ClusterRoleBindings as metadata-only inputs for the Kubernetes secrets/IAM
  source lane.

## Read-only and metadata-only

- The adapter only ever issues `list` calls. There is no method on the surface
  that mutates the cluster.
- Container mapping copies image refs, declared ports, and environment variable
  NAMES only. It records that a container references a secret-backed env var
  (`EnvFromSecret`) without ever copying the value. `env.Value`, secret/configmap
  data, and logs are never read.
- ServiceAccount mapping copies annotation keys, automount posture, bounded
  secret-reference counts, the IRSA role annotation target, and the GKE
  Workload Identity service-account annotation target. The source hashes or
  digests provider targets before fact emission; referenced Secret names and
  token values are never copied.
- RBAC mapping copies bounded verb, API group, resource, and subject-kind
  metadata. Resource names and non-resource URLs are reduced to presence flags;
  role names, binding names, and subject names cross the neutral boundary only
  so the source package can build deterministic fingerprints and join keys.
- A `Forbidden` list result is returned as a partial result with reason
  `forbidden_resource` rather than a hard error, so RBAC gaps on one family do
  not abort the snapshot. A mid-stream failure after some pages degrades to
  `partial_list`.

## CRI-resolved image digest (#5432)

For Pod objects only, the adapter reads `pod.Status.ContainerStatuses[].ImageID`
and `pod.Status.InitContainerStatuses[].ImageID` — the ONLY `.Status` fields the
adapter reads. The `ImageID` is the CRI-resolved digest published by the
container runtime for every container, even for tag-referenced images. The
adapter normalizes it via `kuberneteslive.NormalizeCRIImageID` (strips
`docker-pullable://` / `docker://` / `cri-o://` scheme prefixes, keeps only
`repo@sha256:<digest>` forms) and populates `ContainerSummary.ResolvedImageDigest`
on the matching spec container by name. A digest is a content fingerprint
(metadata), so this does not violate the metadata-only invariant.
Deployments, ReplicaSets, and other workload kinds carry only the pod template
spec — they have no status and therefore no resolved digests.

## RBAC posture

The collector needs only `get`, `list`, and `watch` on the configured resource
families and excludes Secret values by default. Prefer a namespace `RoleBinding`
where namespace-scoped collection is enough; avoid wildcard resources/verbs and
`cluster-admin`.

## Testing

Tests use `k8s.io/client-go/kubernetes/fake` to prove the mapping and redaction
without a real cluster, including a `PrependReactor` that returns a `Forbidden`
error to exercise the partial path.
