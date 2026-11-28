# AGENTS.md - internal/collector/kuberneteslive guidance

## Read First

1. `go/internal/collector/kuberneteslive/README.md` - flow, telemetry, invariants
2. `go/internal/collector/kuberneteslive/source.go` - snapshot orchestration
3. `go/internal/collector/kuberneteslive/builder.go` - object-to-fact mapping
4. `go/internal/collector/kuberneteslive/secrets_iam_observations.go` -
   Kubernetes secrets/IAM source-fact mapping
5. `go/internal/collector/kuberneteslive/envelope.go` - fact envelope contract
6. `go/internal/collector/service.go` - shared collector commit boundary
7. `docs/internal/design/388-kubernetes-live-collector.md` - design and scope
8. `go/internal/telemetry/README.md` - metric and span contract

## Invariants This Package Enforces

- METADATA-ONLY. Never emit Secret values, ConfigMap data payloads, environment
  variable values, or container logs. Only image refs, env var NAMES, ports,
  service account, selector, label metadata, ServiceAccount annotation keys,
  bounded secret-reference counts, RBAC rule summaries, and fingerprinted RBAC
  subject metadata are allowed in payloads. Add a redaction test for any new
  field.
- READ-ONLY. The `Client` interface exposes only list methods. Do not add a
  create, update, patch, delete, exec, attach, portforward, or log method.
- Do not import client-go in this package. The Kubernetes API is the `Client`
  interface; the typed adapter lives in `clientgo`.
- Object identity is `(cluster_id, api_group, version, resource, namespace,
  name, uid)`. Never key identity on the API server URL or labels.
- The generation id must depend only on `cluster_id` and observation time, never
  on partial state, so all facts in a snapshot share one generation id.
- A forbidden or partial list emits a warning and marks the generation partial;
  it must not abort the snapshot or assert completeness.
- This package emits facts only. It must never write graph state or decide
  canonical ownership, drift, effective RBAC, IAM posture, or trust-chain truth.
- Metric labels must not contain namespace names, object names, or image names.

## Common Changes And How To Scope Them

- Add a resource family with a `Source.Next` test that asserts emitted fact
  kinds, scope kind, generation id sharing, and warning behavior, plus a
  `clientgo` adapter test using the fake clientset.
- Add a relationship type by extending `RelationshipType`, the builder edge
  derivation, and a test that proves ambiguity warnings for unresolved targets.
- Add telemetry by updating `source.go`, `go/internal/telemetry`, and the docs
  that list metric type, labels, and purpose.

## Anti-Patterns

- Reading any Secret value, ConfigMap data, env value, or log.
- Writing facts to Postgres or graph directly from this package.
- Inferring cluster identity from the API server URL.
- Treating a forbidden list as "no resources" instead of a partial warning.
- Inventing an owner identity for an owner reference that was not collected.

## Evidence

### StatefulSet, DaemonSet, Job, and CronJob workload kinds (#5433)

- **StatefulSet**: `DesiredReplicas` <- `.Spec.Replicas` (`*int32`, nil when unset); `ReadyReplicas` <- `.Status.ReadyReplicas`; `AvailableReplicas` <- `.Status.AvailableReplicas` — this version of `StatefulSetStatus` DOES carry `AvailableReplicas` (added upstream in 1.22), confirmed by reading the struct directly (`go doc k8s.io/api/apps/v1.StatefulSetStatus` / module-cache source), so it maps directly like Deployment/ReplicaSet, not left nil. `PodPhase` nil.
- **DaemonSet** (design decision, documented on `ListDaemonSets` and in `clientgo/README.md`): `DaemonSetStatus` has no replica-count fields at all (a DaemonSet is scheduled per node, not scaled to a replica count), so its per-node scheduling counts are surfaced as the workload's replica-equivalent on an OBSERVED basis: `DesiredReplicas` <- `.Status.DesiredNumberScheduled`, `ReadyReplicas` <- `.Status.NumberReady`, `AvailableReplicas` <- `.Status.NumberAvailable`. These are int32 (not pointers) upstream, so an observed all-zero DaemonSet (no nodes matched yet) reports zero, not nil — proved by `TestAdapterDaemonSetZeroSchedulingCountsAreObserved`.
- **Job**: no replica concept; `DesiredReplicas`/`ReadyReplicas`/`AvailableReplicas`/`PodPhase` all nil, only the pod template spec is emitted via `workloadFromPodSpec` on `.Spec.Template.Spec`.
- **CronJob**: same as Job (all status nil); the pod template is read from the nested `.Spec.JobTemplate.Spec.Template.Spec`, proved by `TestAdapterMapsCronJobNestedPodSpecOnlyNoReplicaStatus`.

New regression tests in `clientgo/client_workload_kinds_test.go` mirror `client_runtime_status_test.go`: `TestAdapterMapsStatefulSetReplicaStatus`, `TestAdapterStatefulSetNilSpecReplicasLeavesDesiredNil`, `TestAdapterMapsDaemonSetSchedulingCounts`, `TestAdapterDaemonSetZeroSchedulingCountsAreObserved`, `TestAdapterMapsJobPodSpecOnlyNoReplicaStatus`, `TestAdapterMapsCronJobNestedPodSpecOnlyNoReplicaStatus`. Redaction tests in `clientgo/client_workload_kinds_redaction_test.go` mirror `client_redaction_test.go`'s METADATA-ONLY proof: `TestPodTemplateRedactionExcludesStatefulSetStatusDetail` and `TestPodTemplateRedactionExcludesDaemonSetStatusDetail` plant sentinel strings across every non-mapped `.Status` field (conditions, revisions, scheduling/misscheduled counts, observed generation, collision count) and prove the emitted payload carries only the mapped replica values, no sentinel leakage. `source_test.go`'s `TestSourceHappyPathEmitsTypedFacts` now also feeds a StatefulSet through the real `Source.Next` path end to end (3 `pod_template` facts). `clientgo/client.go` stayed under the 500-line cap by moving the 4 new `List*` methods into a new sibling file, `clientgo/client_workload_kinds.go` (140 lines); `client.go` is back to 390 lines (net zero after the extraction).

`builder.go`'s `collectWorkloads` lists all 4 new kinds via the same `paginate` bounded-continuation-token path used by the pre-existing 3; `ownerScope` gained `statefulset`/`daemonset`/`job`/`cronjob` cases so owner-reference resolution (e.g. a Pod owned by a Job) reports the correct resource-scope warning instead of falling through to the raw lowercase kind string. `envelope.go` needed no change (kind-agnostic); the `secrets_iam_observations.go` lane is untouched (RBAC/ServiceAccount collection is orthogonal to workload kinds).

Golden-corpus lockstep: `testdata/cassettes/kuberneteslive/supply-chain-demo.json` gains 2 new `kubernetes_live.pod_template` facts (schema_version 1.2.0, unchanged) — a StatefulSet with replica status (`desired_replicas: 2, ready_replicas: 2, available_replicas: 2`) and a DaemonSet with scheduling-count status mapped onto the replica fields (`desired_replicas: 3, ready_replicas: 3, available_replicas: 2`), both using images (`ghcr.io/eshu-hq/internal-tools/{db-migrator,node-agent}:1.0.0`) that do not correlate to the `ociregistry` cassette's single manifest, so the existing `RUNS_IMAGE` evidence (min 3, rc-153) is unaffected. Job/CronJob get no cassette fixture (collection-path coverage lands via the clientgo adapter tests instead; both leave `pod_template` status fields nil so there is no new status-mapping shape to fixture). `testdata/golden/e2e-20repo-snapshot.json`'s `node_counts.KubernetesWorkload` bumps `min` 3 -> 5 (the 2 new cassette facts each materialize into a distinct `KubernetesWorkload` node via the existing `KubernetesWorkloadMaterializationHandler`, which is kind-agnostic) with the note updated additively. `go test ./cmd/golden-corpus-gate/ -count=1` (81 passed) and `bash scripts/test-verify-golden-corpus-gate.sh` (pass) both green on this branch; the full Docker `scripts/verify-golden-corpus-gate.sh` run is left to the orchestrator's heavier-gate pass per this session's scope.

RBAC lockstep: `go/cmd/collector-kubernetes-live/README.md`'s RBAC block was already stale before #5433 (missing `serviceaccounts` and the `rbac.authorization.k8s.io` families that #388's secrets/IAM lane had already added) — corrected to list every collected family grouped by API group. The Helm chart's actual `ClusterRole` (`deploy/helm/eshu/templates/rbac-kubernetes-live-collector.yaml`) was ALSO missing `statefulsets`/`daemonsets` (apps) and `jobs`/`cronjobs` (batch) — a real gap, not just a doc gap: an operator deploying the chart today would get `forbidden_resource` warnings forever for the 4 new kinds. Fixed in the same change and verified with `helm template deploy/helm/eshu -f <scratch values with one in_cluster target> -s templates/rbac-kubernetes-live-collector.yaml`, confirming the rendered `ClusterRole` rules include all 4 new resources under the correct API groups.

### selector_match Service->Pod relationship edges (#5437)

Cold-review follow-up (4 findings fixed in a follow-on change): (1) the golden-corpus gate's `list_kubernetes_correlations`/`GET /api/v0/kubernetes/correlations` query shapes asserted only `minimum_results: 1`, already satisfied by the pre-existing `owner_reference` correlation, so a broken `selector_match` pass would pass undetected — fixed by pinning `required_json_values: {"correlations[].relationship_type": "selector_match"}` on both shapes (`testdata/golden/e2e-20repo-snapshot.json`), proven to gate by a differential: green with the cassette's two `selector_match` facts present, red when temporarily removed. (2) no test used a cross-namespace Service/Pod pair, so `matchSelectors`'s namespace guard had zero regression coverage — fixed with `TestSourceSelectorMatchNamespaceIsolation` (Service ns=`a`, Pod ns=`b`, labels satisfy the selector), proven RED by temporarily deleting the guard. (3) `selectorMatchesLabels`'s `len(selector)==0` branch was provably unreachable from `matchSelectors` (`collectServices` already excludes empty selectors) but untested in isolation — fixed with a direct unit test `TestSelectorMatchesLabelsEmptySelectorNeverMatches` (proven RED by deleting the guard) plus a builder-level `TestSourceHeadlessServiceEmitsNoSelectorMatchEdge` proving the exclusion holds end to end. (4) `matchSelectors` was an unbenchmarked full cross product of every Service against every Pod in the cluster, with the namespace check inside the inner loop (not reducing iteration) — fixed by bucketing `podLabelIndex` from `[]podLabelEntry` to `map[string][]podLabelEntry` keyed by namespace (built in `collectWorkloads`), so `matchSelectors` looks up only the matching namespace's Pods per Service.
