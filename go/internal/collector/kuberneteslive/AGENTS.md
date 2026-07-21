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

### CRI-resolved image digest from pod containerStatuses ImageID (#5432)

No-Regression Evidence: `go test ./internal/collector/kuberneteslive/... ./internal/reducer/... -count=1` passes with byte-identical behavior for all pre-existing correlation paths. The CRI-digest path is additive — when no resolved digest exists (Deployments, ReplicaSets, pending pods), behavior stays byte-identical to before. Five new regression tests (`kubernetes_correlation_cri_digest_test.go`) prove: (1) tag-form ref with CRI digest + matching source → exact, edge-eligible; (2) tag without CRI digest stays derived/provenance-only; (3) CRI digest without source observation → unresolved, never tag-derived; (4) CRI-digest-promoted workload produces a RUNS_IMAGE edge; (5) tag without CRI digest produces no edge. Collector tests (`TestAdapterMapsPodContainerStatusDigest`, `TestAdapterDeploymentHasNoResolvedDigest`, `TestNormalizeCRIImageID`) prove the mapping and normalization. Cardinality shim (`TestDigestJoinCardinalityShim`, coherent 6-ref fixture using the same repository for all refs and source manifests): 33% edge-eligible before (2 digest-pinned refs) → 50% edge-eligible after (2 digest-pinned + 1 CRI-digest-promoted tag ref). The B-7 golden-corpus gate unit tests pass (`test-verify-golden-corpus-gate.sh`, `go test ./cmd/golden-corpus-gate/`). Cassette updated with tag-referenced Pod + resolved digest (only on the Pod; Deployment/ReplicaSet entries carry no resolved_image_digest — they use workloadFromPodSpec which never reads status). schema_version 1.1.0 registered in specs/fact-kind-registry.v1.yaml via schema_version_overrides. B-12 snapshot gains rc-153 (RUNS_IMAGE min ≥ 3, non-vacuous). Full Docker B-7 gate run on this branch head: `scripts/verify-golden-corpus-gate.sh` PASS (433 pass, 0 required-fail, 0 advisory-warn; rc-153 RUNS_IMAGE count=3 ≥ 3, KubernetesWorkload nodes=3).
No-Observability-Change: No new metric instrument, metric label, span, structured log field, status field, queue domain, worker count, batch size, or runtime knob. The `resolved_image_digest` payload field is a new optional key on `kubernetes_live.pod_template` containers — malformed values surface through the existing `input_invalid` dead-letter path. The CRI-digest promotion to exact reuses the existing `materialized[digest]` tally and `kubernetes correlation materialization completed` log. The adapter's only `.Status` reads are the pod `ImageID` (this entry, #5432) plus, added by #5431 and described in the entry below, the pod `.Status.Phase` and the Deployment/ReplicaSet `.Status.ReadyReplicas`/`.Status.AvailableReplicas`. Each is metadata — a content fingerprint or an observed count/phase, never a Secret, env, or log value — so the metadata-only invariant is preserved.

### Observed-vs-desired runtime-status fields on pod_template (#5431)

No-Regression Evidence: additive optional workload-level fields only — `desired_replicas`, `ready_replicas`, `available_replicas`, `pod_phase` — on `kuberneteslivev1.PodTemplate`. No existing field, JSON key, or emission path changes; `go test ./internal/collector/kuberneteslive/... ./internal/facts -count=1` passes byte-identical for every pre-existing path. New regression tests: `TestAdapterMapsDeploymentReplicaStatus` and `TestAdapterMapsReplicaSetReplicaStatus` prove `.Spec.Replicas`/`.Status.ReadyReplicas`/`.Status.AvailableReplicas` map onto the workload with `PodPhase` nil; `TestAdapterDeploymentNilSpecReplicasLeavesDesiredNil` proves an unset `Spec.Replicas` leaves `DesiredReplicas` nil rather than fabricating zero; `TestAdapterMapsPodPhase` proves `.Status.Phase` maps onto `PodPhase` with all three replica fields nil; `TestAdapterPodEmptyPhaseLeavesPodPhaseNil` proves an empty phase leaves `PodPhase` nil rather than emitting an empty string. `TestNewPodTemplateEnvelopeEmitsRuntimeStatus` and `TestNewPodTemplateEnvelopePodPhaseOmittedWhenAbsentReplicasSet` lock the encode-seam wire contract (a field added to the struct but not `EncodeKubernetesLivePodTemplate` would be silently dropped). schema_version bumped 1.1.0 -> 1.2.0 (minor, additive) in `specs/fact-kind-registry.v1.yaml` via `schema_version_overrides`. Cassette (`testdata/cassettes/kuberneteslive/supply-chain-demo.json`) updated: all 3 `pod_template` facts bumped to schema_version 1.2.0; the deployment and replicaset facts gain `desired_replicas`/`ready_replicas`/`available_replicas`; the pod fact gains `pod_phase`. B-12 snapshot (`testdata/golden/e2e-20repo-snapshot.json`) note updated additively on `node_counts.KubernetesWorkload` — no node or edge count change, this is fact-level emission only; no reducer, graph writer, or query surface is touched (deferred to the #5435 materialization capstone). `go test ./cmd/golden-corpus-gate/ -count=1`, `bash scripts/test-verify-golden-corpus-gate.sh`, `go test ./cmd/fact-kind-registry/... -count=1`, `bash scripts/verify-fact-kind-registry.sh`, and `bash scripts/test-verify-fact-kind-registry.sh` all pass. JSON Schema regenerated via `go run ./internal/schemagen/cmd` from the `sdk/go/factschema` module root (not hand-edited); the embedded `fixturepack/schema` copy refreshed via `cp`.
No-Observability-Change: No new metric instrument, metric label, span, structured log field, status field, queue domain, worker count, batch size, or runtime knob. The four new payload fields are optional keys on `kubernetes_live.pod_template` — a missing or malformed value surfaces through the existing `input_invalid` dead-letter path, not a new one. No reducer, graph write, or query handler reads these fields yet, so there is no new signal to instrument.
