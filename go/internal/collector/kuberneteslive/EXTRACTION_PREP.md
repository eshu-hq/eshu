# Kubernetes Live Collector — Extraction Prep Census (`#8859`)

Status: **PREP ONLY**. No code moves, no repo creation, no deploy, no in-tree
removal. This manifest records the current in-tree truth so the cutover gate
(`#4047`) can compare in-tree and external outputs on identical inputs.

Scope: `go/cmd/collector-kubernetes-live/` and
`go/internal/collector/kuberneteslive/` (including `clientgo/`). Out of scope:
`sdk/go` (Codex `#8851`), all other collectors, org repos, deployment.
`go/internal/collector/access/posture/` is a shared dependency, not owned by
this lane: this PREP pins its observed behavior but makes no change there.

## 1. Current packages

### Binary: `go/cmd/collector-kubernetes-live/`

| File | Role | Key symbol |
| --- | --- | --- |
| `main.go:75,98` | entry point, `-mode=live\|cassette\|record` | `parseArgs`, `run`, `runRecord` |
| `config.go:52` | env config, `clustersJSON` + auth map | `loadRuntimeConfig`, `mapAuth` |
| `service.go:45` | live wiring | `buildCollectorService`, `newLiveSource` |
| `service.go:71` | credential-free replay wiring | `buildCassetteService` |
| `service.go:101` | per-cluster client factory | `clusterClientFactory` |

There is deliberately **no** claimed/claim-driven wiring: claim-driven
collection is deferred (README `Deferred`, cmd `AGENTS.md`). The binary runs
`collector.Service` (poll) only.

### Library: `go/internal/collector/kuberneteslive/`

| File | Role | Key symbol |
| --- | --- | --- |
| `types.go:19` | collector family name | `CollectorKind = "kubernetes_live"` |
| `types.go:27` | schema version constant | `SchemaVersion = "1.0.0"` (see §2) |
| `types.go:32-63` | resource-scope telemetry enum | `ResourceScopeNamespaces` … |
| `types.go:67-81` | warning reason enum | `WarningForbiddenResource` … |
| `types.go:86-99` | relationship edge enum (3) | `RelationshipOwnerReference`, `RelationshipIngressToService`, `RelationshipSelectorMatch` |
| `types.go:104` | cluster target | `ClusterTarget` |
| `source.go:76` | poll source, one generation per cluster | `Source.Next` |
| `source.go:150` | scope+generation derivation | `scopeAndGeneration` |
| `builder.go:67` | snapshot orchestration | `generationBuilder.run` |
| `builder.go:145` | workload listing (7 kinds) | `collectWorkloads` |
| `client.go:174` | read-only API seam (15 list methods) | `Client`, `ClientFactory` |
| `envelope.go:116,180,233` | pod-template/relationship/warning envelopes | `NewPodTemplateEnvelope` … |
| `envelope_namespace.go:36` | namespace envelope | `NewNamespaceEnvelope` |
| `envelope.go:442` | CRI digest normalization | `NormalizeCRIImageID` |
| `identity.go:18,77,92` | stable identity/scope/generation | `ObjectIdentity`, `ClusterScopeID`, `clusterGenerationID` |
| `selector_match.go` | Service→Pod selector edges | `matchSelectors` |
| `secrets_iam_observations.go` | posture fact mapping | `collectServiceAccounts`, `collectRBAC` |
| `source_parity_8859_test.go` | parity pin (AC source parity) | `TestKubernetesLive8859*` |
| `testdata/8859-parity-kubernetes-live.json` | parity fixture pack | 2 scopes |

### Adapter: `go/internal/collector/kuberneteslive/clientgo/`

| File | Role | Key symbol |
| --- | --- | --- |
| `client.go` | typed client-go adapter, paginated lists | `Adapter`, `paginate` |
| `client_workload_kinds.go` | StatefulSet/DaemonSet/Job/CronJob lists | `ListStatefulSets` … |
| `client.go:393-429` | annotation capture allowlist | `identityAnnotationAllowlist`, `filterIdentityAnnotations` |
| `identity_rbac.go` | RBAC fingerprinting | identity helpers |
| `config.go` | auth shapes | `AuthModeInCluster`, `AuthModeKubeconfig` |

The core package never imports client-go; the Kubernetes API is the `Client`
interface. Annotation allowlisting is adapter-side: the core passes
`WorkloadObject.Meta.Annotations` through verbatim.

## 2. Fact kinds and versions

`kubernetes_live` family (`go/internal/facts/cloud/kubernetes_live.go`):

| Kind | Version | Constructor |
| --- | --- | --- |
| `kubernetes_live.namespace` | `1.0.0` | `envelope_namespace.go:36` |
| `kubernetes_live.pod_template` | `1.3.0` | `envelope.go:116` |
| `kubernetes_live.relationship` | `1.0.0` | `envelope.go:180` |
| `kubernetes_live.warning` | `1.0.0` | `envelope.go:233` |

`pod_template` bump history: `1.0.0` → `1.1.0` (`resolved_image_digest`,
`#5432`) → `1.2.0` (runtime-status fields, `#5431`) → `1.3.0`
(`annotations`, `#5471` F2). Family declaration:
`kubernetes_live.go:91-112` (`KubernetesLiveFamilyDeclaration`); the producer
record is already cutover-shaped (`kubernetes_live.go:110`):
`collector-kubernetes-live`, authority `core_issued_grant`, all 4 kinds,
planned repo `eshu-hq/eshu-collector-kubernetes-live`, correct in-tree path.

Secrets/IAM posture facts emitted through the shared `access/posture` seam
(all schema `1.0.0`, `SecretsIAMSchemaVersionV1`, confidence `reported`):

| Kind | Emitted when |
| --- | --- |
| `k8s_service_account` | every listed ServiceAccount |
| `k8s_service_account_token_posture` | every listed ServiceAccount |
| `k8s_rbac_role` | every listed Role/ClusterRole |
| `k8s_rbac_binding` | every listed RoleBinding/ClusterRoleBinding |
| `k8s_workload_identity_use` | every workload with a service account |
| `k8s_gcp_workload_identity_binding` | GKE annotation + cluster declares `GCPWorkloadPool` |
| `eks_irsa_annotation` | ServiceAccount carries the IRSA annotation |
| `secrets_iam_coverage_warning` | any partial/forbidden family list |

`eks_pod_identity_association` exists in the shared seam but this collector
never emits it. Posture facts carry `CollectorKind = "secrets_iam_posture"`,
not `"kubernetes_live"` (pinned by `parity8859WantCollectorKind`); the
extracted producer must preserve both.

## 3. Consumers (must agree at cutover)

- Reducer domain: `kubernetes_correlation`
  (`go/internal/reducer/kubernetescorrelation/`), including `RUNS_IMAGE`
  materialization and the `selector_match`/`owner_reference` identity-edge
  classifier.
- Reducer domain: `secretsiam` trust chain (`go/internal/reducer/secretsiam/`)
  for the posture facts.
- Projector intents: `go/internal/projector/kubernetes/` (namespace and
  workload materialization, correlation intents); generic decode
  `go/internal/reducer/schemadecode/factschema_decode_kuberneteslive.go`.
- Graph writers: `go/internal/storage/cypher/kubernetes/`,
  `go/internal/storage/cypher/edge/writer/kubernetes_correlation.go`.
- Query/read: `GET /api/v0/kubernetes/correlations`
  (`go/internal/query/kubernetes/handler.go`),
  `POST /api/v0/infra/relationships` (`what_runs_image`).
- MCP: `list_kubernetes_correlations` (`go/internal/mcp/kubernetes/`).
- Golden fixture: `testdata/cassettes/kuberneteslive/supply-chain-demo.json`
  (B-7 gate; B-12 `node_counts.KubernetesWorkload` min 5, rc-153 `RUNS_IMAGE`
  ≥ 3). The parity pack in `testdata/8859-parity-kubernetes-live.json` is the
  `#4047` gate input (AC source parity, in-tree side).

## 4. Durable-state owner

The collector owns **no** durable state. Durable writes stay core-owned:

- Fact commit: `postgres.NewIngestionStore` (`service.go:56`).
- Poll loop: `collector.Service` (no `ClaimedService`, no lease, no heartbeat).
- In-tree transient state only: `Source.next` cursor (`source.go:71`); resets
  per batch and never persists.

Admission, persistence, reducer queue, projection, and retraction remain Eshu
core per the extraction policy. The external producer must submit a bounded
result; it never receives Postgres, graph, queue, lease, reducer, API, MCP, or
workflow handles.

## 5. Protocols

- Kubernetes List API via client-go: 15 read-only list methods plus
  `PingReadOnly` (`client.go:174-192`). No watch, no create/update/patch/
  delete/exec/attach/portforward/logs anywhere in the lane.
- Pagination: page size 200 with continue tokens (`paginate`).
- Auth: `kubeconfig` (requires `kubeconfig_path`, optional `kube_context`) or
  `in_cluster` (mounted service-account token); per-cluster QPS/burst.
- Target wire shape: `ESHU_KUBERNETES_LIVE_CLUSTERS_JSON` decoded by
  `loadRuntimeConfig` (`config.go:52-105`); runtime shape `ClusterTarget`
  (`types.go:104-124`). `cluster_id` is operator-declared and durable, never
  inferred from the API server URL.
- Public target contract: `collector-sdk/v1alpha1`
  (`sdk/go/collector/types.go:10`).
- Cassette replay/record: binary flags `-mode=cassette|record
  -cassette-file=<path>` (`main.go`).

## 6. Artifacts

- Today: single in-tree binary `collector-kubernetes-live` (`serviceName` in
  `main.go`), registered in `scripts/install-local-binaries.sh`. Helm chart
  templates exist but the lane is gated off by default
  (`kubernetesLiveCollector.enabled: false`); not added to any Compose
  service. No published artifact.
- At cutover: digest-pinned OCI artifact executed via `OCIRunner`, same SDK
  contract as `ProcessRunner`. No artifact is built or published by this PREP.

## 7. Migration stage

`planned` (extraction catalog row `scope.CollectorKubernetesLive`,
`coreCriteria()`, `CorrelationCritical: true`). Not `ready_for_cutover`:
repository, release, operational, and rollback proof are absent; claim-driven
collection is unimplemented in-tree; and the public SDK surface is owned by
`#8851`. Roadmap owners: `#6707` inventory, `#6708` SDK/fact-schema, `#6709`
producer delegation, `#6710` template and compatibility harness, `#4047`
per-collector cutover gate.

## 8. Prerequisites for cutover

1. Published SDK + fact-schema modules (`#6708`, `#8851` Codex lane) at the
   pinned `v0.1.0-rc.1` pair (verified covering every seam in
   [SHIM_MAP](SHIM_MAP.md); no gaps, no handoff needed).
2. First-party producer delegation live (`#6709`); grant draft recorded in
   [PRODUCER_GRANT_DRAFT](PRODUCER_GRANT_DRAFT.md).
3. Collector repository from the `#6710` template with the compatibility
   harness matrix (newest collector × oldest core and reverse).
4. Parity proof on representative inputs via the fixture pack in
   `testdata/8859-parity-kubernetes-live.json` and
   `source_parity_8859_test.go` (AC source parity, in-tree side).
5. Producer authority single-holder enforcement and rehearsed rollback
   ([ROLLBACK_DRAFT](ROLLBACK_DRAFT.md)).
6. Internal→public shim map agreed ([SHIM_MAP](SHIM_MAP.md)), including the
   unimplemented claim seam and the posture SourceURI hardening gap (§10).
7. A future in-tree scheduler, if claim-driven collection ever lands, must
   use the shared `inTreeSchedulable` pre-filter (`#8919`, CLOSED): no
   kubernetes-live scheduler exists today, so the exclusion holds vacuously,
   and the registry test fails any new `shouldSchedule*` selector that skips
   the shared rule.

## 9. `#8859` acceptance sweep (this PREP)

Already met by this change or prior merged work:

- Package/fact/consumer/durable-state/protocol/artifact census: this file.
- Source parity pin on representative + hostile inputs: `source_parity_8859_test.go`
  (4 tests) + fixture pack. Covers all 4 `kubernetes_live` kinds and 5 posture
  kinds, duplicate stability, grant-free emission, partial/forbidden handling,
  unresolvable-owner ambiguity, and credential-URI sanitization.
- Scheduler exclusion (`#8919`): verified — no kubernetes-live scheduler
  exists; the shared `inTreeSchedulable` rule covers any future one.
- SDK seam coverage at `v0.1.0-rc.1`: verified byte-identical for
  `collector/types.go` and every `factschema` encode used (modulo the
  expected `eshu-sdk` vs in-tree import path). See SHIM_MAP §7.
- Producer record cutover-shaped in the family declaration (planned repo,
  `core_issued_grant`).

Still open (need deployment, release, or cutover; NOT this PREP):

- Exact admitted facts / reducer / graph / HTTP / MCP truth on the extracted
  path; concurrency/duplicate/ordering/cancellation/revoked-authority/stale-
  claim/partial proof on the extracted path; bounded-resource and
  observability proof on the extracted path.
- Credential-handling closure for the posture SourceURI gap (§10).
- Independent build/release/deploy on public contracts; oldest/newest version
  matrix; rehearsed production rollback; independent review; stable CI at the
  cutover head; in-tree removal after production acceptance.
- Live-backend proof: NOT_CHECKED — AWS SSO expired and no kind cluster on
  this machine.

## 10. Credential handling (recorded, one gap open)

- Cluster credentials travel by reference only (`kubeconfig_path`, in-cluster
  mount, QPS/burst); never logged, metered, or written into facts (binary and
  package `AGENTS.md` invariants; `credential_canary_test.go`).
- Container payloads carry image refs, port numbers, and env var NAMES only;
  Secret values, ConfigMap data, env values, and logs have no model field.
- Identity-bearing names (ServiceAccount, RBAC subjects, UIDs, resource
  versions) are fingerprinted before posture emission; raw names live only
  long enough to form deterministic join keys. The IRSA role ARN is carried
  raw in `eks_irsa_annotation.role_arn` (join evidence against AWS IAM facts).
- The `kubernetes_live` envelope seam strips userinfo and secret query keys
  from `source_uri` (pinned byte-exact in the hostile test).
- **Open gap**: the shared posture seam (`access/posture` `newEnvelope`)
  only trims `source_uri`, so `secrets_iam_coverage_warning` facts carry an
  operator-declared `source_uri` verbatim. Pinned (not approved) in
  `TestKubernetesLive8859ParityHostileInputs`. The extracted producer must
  sanitize or drop `SourceURI` for posture facts; fixing the shared seam
  belongs to the secrets-IAM lane.

## 11. Fixture-scope note (recorder belt)

The parity pack covers namespaces, pods, deployments, replicasets,
statefulsets, services (selector_match), ServiceAccounts, and the hostile
partial/forbidden path. Ingress (`networking.k8s.io`) and RBAC
(`rbac.authorization.k8s.io`) group strings are not documented safe host
forms, so the recorder's private-data belt
(`recordpseudo.Verify`, mirroring `scripts/lib/cassette_private_data_pattern.sh`)
refuses a cassette containing them — verified empirically while cutting this
pack. Those emission paths stay pinned by `TestSourceHappyPathEmitsTypedFacts`
(ingress edges) and `secrets_iam_source_test.go` (RBAC). Extending the shared
allowlist is a reviewed cross-lane edit, out of scope for this PREP.
