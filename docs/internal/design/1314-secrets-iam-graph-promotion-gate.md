# Secrets/IAM Graph Promotion ADR And Schema Gate

Status: **ADR APPROVED - SCHEMA AND TARGET ACTIVATION PENDING.** The DDL,
writer, fixture proof, benchmark proof, repo-local NornicDB/Neo4j conformance
evidence, and section 14 principal/security approval are present. The
projection remains default off until `risk:schema` approval, a target deployment
decision, and flag-on activation proof are recorded.

Issue: #1314. Parent: #25. Depends on the #1313 reducer read-model slice.

## 1. Decision

Approve graph promotion only after the `secrets_iam_posture` reducer read model
has proven exact, stale, partial, permission-hidden, and unsupported states. The
graph projection must consume reducer-owned read-model facts only. It must not
join raw AWS IAM, Kubernetes, or Vault source facts directly into graph truth.

The gated graph implementation is a default-off projection from exact read-model
rows into a redaction-safe graph path:

```text
(:KubernetesWorkload {uid})            optional, only if already materialized
  -[:SECRETS_IAM_USES_SERVICE_ACCOUNT]->
(:SecretsIAMServiceAccount {uid})
  -[:SECRETS_IAM_ASSUMES_IAM_ROLE]->
(:CloudResource {uid})                 existing aws_iam_role node only

(:SecretsIAMServiceAccount {uid})
  -[:SECRETS_IAM_AUTHENTICATES_TO_VAULT_ROLE]->
(:SecretsIAMVaultAuthRole {uid})
  -[:SECRETS_IAM_USES_VAULT_POLICY]->
(:SecretsIAMVaultPolicy {uid})
  -[:SECRETS_IAM_GRANTS_SECRET_READ]->
(:SecretsIAMSecretMetadataPath {uid})
```

The relationship names are intentionally static and closed vocabulary. No edge
type may encode action names, policy names, namespaces, service-account names,
Vault paths, ARNs, or provider-specific values.

## 2. Current State

The Secrets/IAM source lanes emit redacted AWS IAM, Kubernetes, and Vault facts.
The #1313 read-model slice adds reducer-owned facts for:

- `reducer_secrets_iam_identity_trust_chain`
- `reducer_secrets_iam_privilege_posture_observation`
- `reducer_secrets_iam_secret_access_path`
- `reducer_secrets_iam_posture_gap`

That reducer layer is the accuracy boundary. It performs cross-source joins,
freshness comparison, exact-vs-partial classification, and unsupported-layer
labeling before any graph projection is allowed.

### 2.1. Prerequisite status

The non-graph prerequisites this gate depends on are merged. This subsection is
a status snapshot. Section 14 records the principal/security decision; target
deployment flag-on still requires separate activation proof.

Merged to `main`:

- Source lanes emit redacted facts: AWS IAM (`awscloud/services/iam`,
  `accessanalyzer`), Kubernetes RBAC (`kuberneteslive`), and Vault metadata
  (the `secretsiam` builders + `vaultlive` source, all seven Vault fact families,
  #1355).
- The reducer read model (#1313/#1327) builds and persists the four
  `reducer_secrets_iam_*` fact kinds with the six-state vocabulary
  (`exact`/`partial`/`unresolved`/`stale`/`permission_hidden`/`unsupported`).
- The query + MCP endpoints over the four Secrets/IAM read models.
- The metadata-only redaction contract this ADR's §7 requires, enforced at the
  `secretsiam` envelope chokepoint.
- The default-off graph projection DDL, writer, reducer domain, endpoint
  readiness gate, retry liveness handling, and repo-local proof artifacts.

What remains gated after the section 14 approval: `risk:schema` approval,
target deployment activation, and any production claim of graph projection
authority.

## 3. Non-Goals

This ADR does not approve:

- graph projection from source facts without reducer read-model admission
- DDL or graph writer code in this PR
- API or MCP answers that claim graph authority before graph proof exists
- complete IAM/Vault/Kubernetes effective permission evaluation
- raw secret values, Vault paths, policy bodies, token claims, role ARNs,
  namespaces, service-account names, URLs, or credential-bearing values as graph
  properties
- projection of `partial`, `unresolved`, `stale`, `permission_hidden`, or
  `unsupported` rows as graph edges

## 4. Admission Rules

Only exact reducer facts can promote.

| Read-model row | Graph admission |
| --- | --- |
| `identity_trust_chain` with `state=exact` | May create the ServiceAccount node and exact ServiceAccount-to-IAM-role / ServiceAccount-to-Vault-role edges when endpoints resolve. |
| `secret_access_path` with `state=exact` | May create Vault policy / secret metadata path nodes and Vault policy access edges. |
| `privilege_posture_observation` | Provenance-only in Postgres/API. No graph edge in the first graph PR. |
| `posture_gap` | Provenance-only in Postgres/API. No graph edge. |
| Any non-exact state | No graph edge and no graph node creation from that row. |

If one endpoint is missing, the edge is skipped and counted. The projection must
never fabricate a `CloudResource`, `KubernetesWorkload`, Vault node, or secret
metadata node to make a chain look complete.

## 5. Node Identity And Schema Proposal

All new node identities are redaction-safe values already present in the reducer
read model.

| Label | `uid` | Properties allowed in the first graph PR |
| --- | --- | --- |
| `SecretsIAMServiceAccount` | `service_account_join_key` | `uid`, `scope_id`, `generation_id`, `evidence_source`, `confidence` |
| `SecretsIAMVaultAuthRole` | `vault_role_join_key` | `uid`, `vault_mount_join_key`, `scope_id`, `generation_id`, `evidence_source`, `confidence` |
| `SecretsIAMVaultPolicy` | `vault_policy_join_key` | `uid`, `scope_id`, `generation_id`, `evidence_source`, `confidence` |
| `SecretsIAMSecretMetadataPath` | stable id over `vault_mount_join_key` and `kv_path_fingerprint` | `uid`, `vault_mount_join_key`, `kv_path_fingerprint`, `scope_id`, `generation_id`, `evidence_source`, `confidence` |

IAM role nodes should reuse existing `CloudResource` nodes for scanned
`aws_iam_role` resources. A missing IAM `CloudResource` endpoint is
`skipped_unresolved`, not a new `IAMRole` node. This avoids a duplicate IAM-role
keyspace and keeps AWS blast-radius edges (`CAN_ASSUME`, `CAN_PERFORM`,
`CAN_ESCALATE_TO`) on one canonical IAM role identity.

Kubernetes workload edges should target existing `KubernetesWorkload` nodes only
when the reducer row's `workload_object_id` resolves to the
`kubernetes_workload_uid` keyspace. A missing workload endpoint is a skip, not a
fabricated workload node. The ServiceAccount subgraph is still useful without
the optional workload edge.

Schema DDL for the first build PR should add unique `uid` constraints and
lookup indexes for only the new `SecretsIAM*` labels. It should reuse existing
`CloudResource.uid` and `KubernetesWorkload.uid` constraints for external
endpoints. DDL must run before writes through schema bootstrap and must not use
drop/create cycles on a live NornicDB store.

### 5.1. Endpoint join feasibility (implementation finding, #1347)

A read of the current read-model build (`secrets_iam_trust_chain_build.go`) and
the canonical node keyspaces establishes which external endpoints are resolvable
from the read model today. This corrects the build plan; promoting an edge whose
endpoint cannot be resolved would force a fabricated join, which §4 forbids.

- **`KubernetesWorkload` endpoint — RESOLVABLE.** The read model's
  `workload_object_id` is the same keyspace as the live `KubernetesWorkload`
  node `uid` (the Kubernetes correlation edge writer already treats
  `workload_uid = workload_object_id`). So `SECRETS_IAM_USES_SERVICE_ACCOUNT`
  can `MATCH (:KubernetesWorkload {uid: workload_object_id})` and skip-count when
  absent.
- **IAM-role `CloudResource` endpoint — RESOLVABLE (issue #1379), with one
  bounded caveat.** The `iam_role_fingerprint` is `secretsIAMFingerprint(
  "iam_role", role_arn)` — a deterministic, unkeyed SHA-256 over canonical JSON
  (via `facts.StableID`). It provides redaction (no raw ARN in the graph) but
  cannot join to the IAM-role `CloudResource` `uid`, built from
  `cloudResourceUID(account_id, region, "aws_iam_role", role_arn)` where the AWS
  resource collector sets `resource_id = role_arn` (`services/iam`
  `roleObservation`). The only uid inputs not in the role ARN are the AWS scan
  boundary `account_id` and `region`. Both are carried by the `aws_iam_principal`
  source fact that the trust-chain build (`secretsIAMExactChains`) already
  requires for the chain to be exact, so the read model now **additionally
  carries `iam_role_cloud_resource_uid`** — the redaction-safe CloudResource uid
  recomputed at the existing build site from a fact already in hand (no new
  collector, no new source field, no new cross-source join). The raw ARN is never
  stored; it is only hashed into the one-way uid, exactly as the AWS resource
  projection and the `iam_can_assume` edge slice compute it. When the principal
  fact omits `account_id`/`region` the uid stays blank and the edge keeps the
  skip+count behavior.

  Caveat (bounded, deliberately conservative): the canonical CloudResource uid
  uses the **`awscloud` IAM resource collector's** boundary `region`, while
  `iam_role_cloud_resource_uid` is recomputed from the **`secretsiam`
  collector's** boundary `region`. IAM is global, so both lanes are expected to
  emit the same region literal; the build does **not** parse account/region out
  of the ARN string (a parsed region could diverge from the boundary literal and
  fabricate a non-matching uid). If a deployment's two source lanes ever emit
  divergent region literals for IAM, the recomputed uid will not match the
  CloudResource node and the writer `MATCH` is a no-op (no fabricated node, no
  wrong edge) — it degrades to the prior skip behavior, never to a wrong join.
  The strictly-canonical alternative — having the trust-chain evidence loader
  also load the `aws_resource` IAM-role facts and resolve the uid by ARN the way
  `iam_can_assume` does — remains the upstream follow-up if region-literal
  equality across the two IAM lanes is ever not guaranteed.

Consequently the first graph build implements **five** edges: the four that
always resolve from read-model join keys or the workload uid
(`USES_SERVICE_ACCOUNT`, `AUTHENTICATES_TO_VAULT_ROLE`, `USES_VAULT_POLICY`,
`GRANTS_SECRET_READ`) plus `SECRETS_IAM_ASSUMES_IAM_ROLE` from the
ServiceAccount node to the existing IAM-role `CloudResource` node, emitted only
when `iam_role_cloud_resource_uid` is present. When it is absent the IAM-role
edge is extracted and **counted as a skip** with reason
`iam_role_endpoint_unresolved_pending_read_model`, so the chain is never
fabricated. The edge carries a bounded `assume_mode`
(`web_identity` / `pod_identity`).

## 6. Relationship Contract

| Relationship | Source row | Endpoint rule | Mutable properties |
| --- | --- | --- | --- |
| `SECRETS_IAM_USES_SERVICE_ACCOUNT` | exact `identity_trust_chain` | `KubernetesWorkload` to `SecretsIAMServiceAccount`, only when workload node resolves | `scope_id`, `generation_id`, `evidence_source`, `confidence`, `evidence_fact_ids` |
| `SECRETS_IAM_ASSUMES_IAM_ROLE` | exact `identity_trust_chain` | `SecretsIAMServiceAccount` to existing IAM role `CloudResource`, only when `iam_role_cloud_resource_uid` resolves | `assume_mode`, `scope_id`, `generation_id`, `evidence_source`, `confidence`, `evidence_fact_ids` |
| `SECRETS_IAM_AUTHENTICATES_TO_VAULT_ROLE` | exact `identity_trust_chain` | `SecretsIAMServiceAccount` to `SecretsIAMVaultAuthRole` | `scope_id`, `generation_id`, `evidence_source`, `confidence`, `evidence_fact_ids` |
| `SECRETS_IAM_USES_VAULT_POLICY` | exact `identity_trust_chain` | `SecretsIAMVaultAuthRole` to `SecretsIAMVaultPolicy` | `scope_id`, `generation_id`, `evidence_source`, `confidence`, `evidence_fact_ids` |
| `SECRETS_IAM_GRANTS_SECRET_READ` | exact `secret_access_path` | `SecretsIAMVaultPolicy` to `SecretsIAMSecretMetadataPath` | `capabilities`, `scope_id`, `generation_id`, `evidence_source`, `confidence`, `evidence_fact_ids` |

Relationship `MERGE` identities must use only the two endpoint `uid` values and
the static relationship token. Mutable properties are `SET` after `MERGE`.
Capabilities are a bounded enum list from the reducer read model and must not
include raw Vault path text or policy names.

## 7. Redaction And Security Gate

Graph properties may store only:

- redaction-safe join keys and fingerprints
- bounded enums such as `confidence`, `assume_mode`, and capability class
- scope/generation/evidence-source metadata
- reducer evidence fact IDs

Graph properties must never store:

- secret values or secret names
- cleartext Vault paths or key names
- raw Vault policy bodies or policy names
- IAM policy bodies, condition values, role ARNs, or account-specific OIDC
  subject strings
- Kubernetes namespaces, service-account names, subject names, token claims, or
  projected tokens
- private URLs, credentials, tenant IDs, or token-like strings

Security review must sign off on the property allowlist before schema DDL lands.
Unknown new fields default to "do not project."

## 8. Readiness, Ordering, And Rollback

Status (#1380): cross-scope endpoint readiness is implemented as a **uid-exact**
presence primitive rather than a scope/generation phase, because the endpoint
nodes (`CloudResource`, `KubernetesWorkload`) commit in different reducer scopes
than the projection intent, and the scope/generation-keyed
`graph_projection_phase_state` gate cannot prove a *specific* cross-scope node
committed. The `graph_endpoint_presence(keyspace, uid)` table (Postgres migration
`024`) is written idempotently by the CloudResource and KubernetesWorkload node
materializers (flag-gated, so the default hot path is unchanged), and
`SecretsIAMGraphProjectionHandler` gates on `MissingUIDs` before retract/write,
returning a retryable `secrets_iam_endpoint_not_ready` error so the queue
re-enqueues rather than dropping edges.

Resolved liveness gap (#1391): cross-scope endpoint waits no longer consume the
bounded reducer retry budget. `ReducerQueue.Fail` preserves
`failure_class=secrets_iam_endpoint_not_ready` and requeues with the existing
`visible_at`/`next_attempt_at` backoff even after `ESHU_REDUCER_MAX_ATTEMPTS`.
The single and batch claim SQL then keep `attempt_count` unchanged while that
class is pending, so repeated cold-start readiness waits do not terminally
dead-letter the generation or steal the later retry budget for real execution
failures once endpoints exist. Status, latest-failure, and blockage surfaces
continue to expose the specific failure class for operator diagnosis. See
`go/internal/reducer/README.md` and `go/internal/storage/postgres/README.md`.

The implementation must publish a new canonical-node readiness phase
for the `SecretsIAM*` node keyspace only after node writes commit. Edge writes
must gate on:

- the new Secrets/IAM node keyspace phase
- `cloud_resource_uid/canonical_nodes_committed` for IAM role `CloudResource`
  endpoints when `SECRETS_IAM_ASSUMES_IAM_ROLE` rows exist
- `kubernetes_workload_uid/canonical_nodes_committed` for optional workload
  edges

Conflict domain should be the reducer scope generation plus
`secrets_iam_graph_projection:<scope_id>`. Retract-before-reproject must remove
only reducer-owned `SECRETS_IAM_*` edges and node properties where
`evidence_source='reducer/secrets-iam-graph'` and `scope_id` match. It must
never delete existing `CloudResource` or `KubernetesWorkload` nodes.

Rollback path:

1. Disable the graph domain registration.
2. Retract reducer-owned `SECRETS_IAM_*` edges and reducer-owned properties by
   evidence source and scope.
3. Leave constraints and redacted nodes in place unless a later migration proves
   they are unused.
4. If graph-store corruption or schema rollback is required, rebuild the graph
   backend from Postgres facts after data-plane schema bootstrap. Do not delete
   Postgres facts or queues.

## 9. Concurrency And Retry Contract

The graph writer must be idempotent under duplicate reducer delivery and
concurrent reprojection:

- build rows from sorted reducer read-model facts
- dedupe by `(source_uid, relationship_type, target_uid)`
- use batched `UNWIND $rows AS row`
- `MERGE` nodes by `uid` only
- `MATCH` existing `CloudResource` and `KubernetesWorkload` endpoints by `uid`
- `MERGE` relationships by endpoints and static relationship token only
- `SET` mutable properties after identity `MERGE`

Retries can repeat the whole batch. A commit-time uniqueness conflict from
concurrent `MERGE` must converge through the existing retrying graph executor.
The implementation must not reduce reducer workers, force batch size `1`, or
serialize all graph writes as a substitute for idempotency.

## 10. Backend Compatibility

The build must use Eshu's shared Cypher/Bolt contract:

- backend-neutral writers under `go/internal/storage/cypher`
- no reducer or query-handler branch on `ESHU_GRAPH_BACKEND`
- static labels and relationship types from an allowlist
- `UNWIND` batches with indexed `uid` anchors
- no dynamic relationship-type construction from source data
- no property-keyed relationship identity

NornicDB is the default backend. Neo4j remains the compatibility backend. The
build PR must include focused writer tests that assert the Cypher shape and
backend conformance evidence that the schema bootstrap and writers stay within
the shared contract.

## 11. Fixture And Proof Matrix

Status (#1381): the fixture-truth proof, live writer conformance, shared backend
conformance, and schema readback proofs are attached in
`1314-secrets-iam-graph-promotion-proof-2026-06-07.md`. The full load → extract
→ write orchestration runs through `SecretsIAMGraphProjectionHandler` against the
recording writer in
`go/internal/reducer/secrets_iam_graph_projection_fixture_truth_test.go`,
asserting the exact node/edge rows for all four node families and all five edge
families plus the skip-counted cases (missing workload, missing vault hop,
missing secret path, non-exact states, pod-identity IAM-role-unresolved) and
duplicate-delivery idempotency. The TRUE live-backend conformance is the
BACKEND-GATED `TestSecretsIAMGraphWriterLiveConformance` in
`go/internal/storage/cypher/secrets_iam_graph_live_test.go`; it writes all four
node families and five edges, reads them back, and proves scoped retract leaves
the retained `KubernetesWorkload` and `CloudResource` endpoints intact, but
SKIPs cleanly unless `ESHU_SECRETS_IAM_GRAPH_LIVE=1` and Bolt env are configured
(no fabricated live proof). The June 7 proof ran that gated check against both
NornicDB and Neo4j profiles without enabling production graph projection.

The implementation must prove each row below before graph writes can merge.

| Fixture | Required proof |
| --- | --- |
| exact IRSA chain | ServiceAccount node, IAM role edge, Vault role/policy/path edges all resolve from exact reducer rows. |
| exact EKS Pod Identity chain | IAM role edge uses `assume_mode=pod_identity` and does not require a web-identity subject fingerprint. |
| exact Vault path without workload node | ServiceAccount-to-Vault subgraph projects, workload edge is skipped and counted. |
| name coincidence | no graph edge without reducer exact join keys. |
| wildcard IAM subject | `privilege_posture_observation` remains provenance-only. |
| wildcard Vault selector | provenance-only, no Vault auth role edge. |
| missing IAM CloudResource | no fabricated IAM role node, skip counted. |
| stale generation | no graph edge; stale read-model row remains Postgres/API truth. |
| permission hidden | no graph edge; API/MCP returns partial or unsupported truth label. |
| unsupported SCP/Sentinel/RGP/EGP | no graph edge; unsupported capability label preserved. |
| duplicate delivery | one node/edge identity after retry. |
| retraction | prior generation reducer-owned edges are removed without touching unrelated graph state. |

Graph truth must be checked with direct Cypher queries against both NornicDB and
Neo4j-compatible profiles where feasible. API and MCP read surfaces must agree
with the graph and still surface unsupported/provenance-only states from the
Postgres read model.

## 12. Performance Impact Declaration For The Build PR

Status (#1381): proof-ladder steps 1–4 are built and green, with backend proof
captured in `1314-secrets-iam-graph-promotion-proof-2026-06-07.md`. Step 3, the
writer benchmark, ships as `BenchmarkSecretsIAMGraphWriter`
(`go/internal/storage/cypher/secrets_iam_graph_writer_bench_test.go`): it writes
all four `SecretsIAM*` node families and all five resolvable `SECRETS_IAM_*`
edge families at 5,000 rows each through the no-op group executor, isolating
statement construction and `UNWIND` batching from graph round trips. Measured on
an Apple M4 Pro (`darwin/arm64`, `-benchtime=50x -count=3`): ~20.8–38.7 µs/op,
53,728–53,729 B/op, 765 allocs/op — faster than the shipped
`BenchmarkCloudResourceNodeWriter`
(~2.87 ms/op), `BenchmarkKubernetesCorrelationEdgeWriter` (~1.10 ms/op),
`BenchmarkObservabilityCoverageEdgeWriter` (~1.66 ms/op), and
`BenchmarkSecurityGroupReachabilityWriter` (~3.98 ms/op) on the same 5,000-row
shape. The writer builds in-memory batches once per scope with no per-row
token-grouping and no per-edge graph read, satisfying the §12 "no N+1" contract
and staying far below the ~10% regression stop threshold against the same-shape
baselines. The BACKEND-GATED live writer conformance test skips cleanly without
a configured backend and never fabricates a passing live proof.

Affected stage: reducer-owned graph projection after the
`secrets_iam_trust_chain` read model.

Expected cardinality: one ServiceAccount/VaultAuthRole/VaultPolicy/path node
per exact read-model identity, plus up to five static edge families per exact
chain. It is bounded by reducer read-model output, not by raw source-fact
cartesian products. The writer must build in-memory indexes once per scope and
must not perform per-edge graph reads.

Proof ladder:

1. reducer row extraction tests for positive, negative, ambiguous, stale,
   permission-hidden, unsupported, duplicate, and empty cases
2. Cypher writer tests for node and edge identity, redaction allowlist, scoped
   retract, and static relationship tokens
3. writer benchmark against the shipped `CloudResource` and
   `KubernetesWorkload` node/edge writer shape on 5,000 rows
4. NornicDB and Neo4j schema/bootstrap conformance
5. API/MCP truth check for exact and unsupported paths

Stop threshold: if statement construction or graph execution regresses more
than about 10 percent against the same-shape baseline, stop and profile before
merge.

## 13. Observability Requirement

The implementation must add or reuse bounded telemetry so an operator can answer
at 3 AM:

- how many Secrets/IAM nodes were written
- how many exact edges were written by relationship family
- how many rows skipped and why
- whether graph projection is blocked on Secrets/IAM, CloudResource, or
  KubernetesWorkload readiness
- how long load, extraction, retract, node write, edge write, and ack took

Metric labels must be bounded enums only. Raw paths, role ARNs, namespaces,
service-account names, policy names, and secret identifiers must never become
metric labels, logs, span attributes, or status errors.

## 14. Decision Requested

Approve this gate only if reviewers accept:

1. reducer read-model rows are the only graph admission source
2. exact rows are the only rows eligible for graph edges
3. IAM roles reuse existing `CloudResource` nodes rather than a duplicate
   `IAMRole` keyspace
4. optional workload edges require an existing `KubernetesWorkload` endpoint
5. all non-exact states stay provenance-only in Postgres/API/MCP
6. the implementation carries DDL, writer tests, backend conformance,
   performance evidence, and security review

The section 14 gate is approved. No target deployment should enable live
Secrets/IAM graph projection until `risk:schema` approval, the deployment
decision, and flag-on proof are recorded.

The non-graph prerequisites are merged (see section 2.1). Principal and
security sign-off on the six points above is recorded as approved on
2026-06-07. `risk:schema` approval and target deployment activation proof are
the remaining blockers. Issue #1347 tracks the schema/governance gate; issue
#1381 tracks activation proof. Neither issue should claim the projection is
active until both approvals and the target deployment proof are recorded.

No-Regression Evidence: design-only gate. This PR changes only an internal
design document and adds no Go, Cypher, DDL, queue, runtime, API, MCP, or graph
write code.

No-Observability-Change: design-only gate. Existing telemetry is unchanged; the
future implementation telemetry requirements are listed in section 13.

### 5.2. Evidence for the gated steps 1–3 PR (extraction + DDL + writer + domain)

No-Regression Evidence: the implementation uses in-memory extraction, additive
schema DDL, a backend-neutral Cypher writer, and a reducer projection domain
that stays off until `cmd/reducer` wires a live writer. Extraction is one linear
pass over reducer read-model facts, not raw source-fact cross-products. The
writer uses static labels and relationship tokens, uid-only node `MERGE`,
endpoint `MATCH` before edge `MERGE`, `UNWIND` batches, scoped retract, and
retryable backend dispatch.

Observability Evidence: the projection domain emits the
`reducer.secrets_iam_graph_projection` span and bounded-enum node, edge, and
skip counters. Metric labels are static extractor constants only. The frozen
dimension keys and span contract are asserted in telemetry tests.

### 5.3. Evidence for the IAM-role edge promotion (issue #1379)

This change makes `SECRETS_IAM_ASSUMES_IAM_ROLE` the fifth promotable edge by
carrying a CloudResource-joinable IAM-role identity in the read model (see the
RESOLVABLE finding in §5.1). It adds two optional read-model fields
(`iam_role_cloud_resource_uid`, `iam_role_assume_mode`), recomputed at the
existing trust-chain build site from the `aws_iam_principal` fact already
required for the chain, and extends the extractor + Cypher writer to emit the
edge when the uid is present (skip+count otherwise).

No-Regression Evidence: the read-model build remains a single pass over already
loaded facts. The extractor adds one endpoint-pair-deduped edge family to the
same linear pass, and the writer adds one static-token
`MATCH/MATCH/MERGE` template. Missing `CloudResource` endpoints stay no-op-safe
and exact-only; no IAM role node is fabricated.

Observability Evidence: no new telemetry surface. The edge uses the existing
`eshu_dp_secrets_iam_graph_edges_written_total{edge_type}` counter with
`edge_type=SECRETS_IAM_ASSUMES_IAM_ROLE`, and unresolved IAM-role endpoints use
the existing skipped counter reason.
