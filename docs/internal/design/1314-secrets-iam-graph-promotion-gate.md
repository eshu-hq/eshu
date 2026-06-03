# Secrets/IAM Graph Promotion ADR And Schema Gate

Status: **DESIGN PROPOSAL - needs principal and security review before any
DDL or graph-write implementation.** This note is the issue #1314 gate. It
does not add schema, code, graph labels, graph edges, API authority, or runtime
configuration.

Issue: #1314. Parent: #25. Depends on the #1313 reducer read-model slice.

## 1. Decision

Approve graph promotion only after the `secrets_iam_posture` reducer read model
has proven exact, stale, partial, permission-hidden, and unsupported states. The
graph projection must consume reducer-owned read-model facts only. It must not
join raw AWS IAM, Kubernetes, or Vault source facts directly into graph truth.

The first graph implementation PR, if this ADR is approved, must be a gated
projection from exact read-model rows into a redaction-safe graph path:

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

The non-graph prerequisites this gate depends on are either merged or in open,
reviewed PRs. This subsection is a status snapshot, not a claim that every item
is merged; the gate is ready for the principal and security decision in section
14 once the merged-or-in-review prerequisites land.

Merged to `main`:

- Source lanes emit redacted facts: AWS IAM (`awscloud/services/iam`,
  `accessanalyzer`), Kubernetes RBAC (`kuberneteslive`), and Vault metadata
  (the `secretsiam` builders + `vaultlive` source, all seven Vault fact families,
  #1355).
- The reducer read model (#1313/#1327) builds and persists the four
  `reducer_secrets_iam_*` fact kinds with the six-state vocabulary
  (`exact`/`partial`/`unresolved`/`stale`/`permission_hidden`/`unsupported`).
- The identity-trust-chain query + MCP endpoint over that read model.
- The metadata-only redaction contract this ADR's §7 requires, enforced at the
  `secretsiam` envelope chokepoint.

In open, reviewed PRs (tracked, not yet merged):

- The remaining query/MCP read surface — privilege posture observations, secret
  access paths, posture gaps, and a posture summary — with all non-exact states
  surfaced as provenance-only (the invariant admission rule §4 relies on).
- A live Vault metadata client (`vaultlive/vaultapi`, #1356); the runtime wiring
  (CollectorKind + claim scheduling) and live validation remain open under #1356.
- The cross-family secret-leakage guard (#1348) and the confused-deputy
  `sts:ExternalId` posture rule (#1346).

What remains gated and **not** started, pending the section 14 decision: the
graph DDL, node/edge promotion writer, and any API/MCP claim of graph authority.

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
- **IAM-role `CloudResource` endpoint — NOT RESOLVABLE from the current read
  model.** The read model carries only `iam_role_fingerprint`, defined as
  `secretsIAMFingerprint("iam_role", role_arn)` — a one-way HMAC of the role
  ARN. The IAM-role `CloudResource` `uid` is built from
  `(account_id, region, resource_type, resource_id)` (`cloudResourceUID(...)`).
  A fingerprint cannot join to that uid, and the read model carries neither the
  `CloudResource` uid nor the `(account, region, role-name)` needed to compute
  it. **Therefore `SECRETS_IAM_ASSUMES_IAM_ROLE` cannot promote until the
  read-model `identity_trust_chain` row additionally carries a
  CloudResource-joinable IAM-role identity** (the role's `cloud_resource_uid`,
  or the account/region/resource-id to recompute it). That is an upstream
  reducer/read-model change (and depends on the IAM trust fact carrying the
  role's resource identity in a joinable form), tracked as the prerequisite for
  the IAM-role edge.

Consequently the first graph build implements the resolvable subgraph — the four
`SecretsIAM*` nodes and the four edges `USES_SERVICE_ACCOUNT`,
`AUTHENTICATES_TO_VAULT_ROLE`, `USES_VAULT_POLICY`, `GRANTS_SECRET_READ` (all
endpoints resolve from read-model join keys or the workload uid). The IAM-role
edge is extracted and **counted as a skip** with reason
`iam_role_endpoint_unresolved_pending_read_model` until the upstream field
lands, so the chain is never fabricated.

## 6. Relationship Contract

| Relationship | Source row | Endpoint rule | Mutable properties |
| --- | --- | --- | --- |
| `SECRETS_IAM_USES_SERVICE_ACCOUNT` | exact `identity_trust_chain` | `KubernetesWorkload` to `SecretsIAMServiceAccount`, only when workload node resolves | `scope_id`, `generation_id`, `evidence_source`, `confidence`, `evidence_fact_ids` |
| `SECRETS_IAM_ASSUMES_IAM_ROLE` | exact `identity_trust_chain` | `SecretsIAMServiceAccount` to existing IAM role `CloudResource` | `assume_mode`, `scope_id`, `generation_id`, `evidence_source`, `confidence`, `evidence_fact_ids` |
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

The first implementation PR must publish a new canonical-node readiness phase
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
6. the first implementation PR must carry DDL, writer tests, backend
   conformance, performance evidence, and security review

Until this is approved, no Secrets/IAM graph DDL or graph writes should land.

The non-graph prerequisites are merged or in open reviewed PRs (see section
2.1). Once those land, a principal and security sign-off on the six points above
is the remaining thing blocking the first (separately-reviewed) implementation
PR. Issue #1347 tracks that gated implementation and stays blocked on this
decision; it is not an implementation task until the gate is approved.

No-Regression Evidence: design-only gate. This PR changes only an internal
design document and adds no Go, Cypher, DDL, queue, runtime, API, MCP, or graph
write code.

No-Observability-Change: design-only gate. Existing telemetry is unchanged; the
future implementation telemetry requirements are listed in section 13.

### 5.2. Evidence for the gated steps 1–2 PR (extraction + DDL + writer)

No-Regression Evidence: this PR is pure in-memory extraction, additive schema
DDL, and a backend-neutral Cypher writer that **does not execute against a graph
backend in this PR** — the reducer domain that calls it (load → extract →
retract → write → readiness) is the next gated step.
`ExtractSecretsIAMGraphRows` is a single linear pass over the reducer read-model
facts (bounded by read-model output, not by raw source-fact cross-products),
building deduped, sorted node/edge rows with no I/O. The DDL additions are
`CREATE CONSTRAINT/INDEX ... IF NOT EXISTS` for the four `SecretsIAM*` labels
only — additive, no drop/create, applied idempotently by schema bootstrap before
any write. `SecretsIAMGraphWriter` mirrors the shipped `iam_can_perform`
writer: static labels/relationship tokens (no data-driven Cypher), uid-only node
`MERGE`, `MATCH`/`MATCH`/`MERGE` edges (a missing endpoint is a no-op, never a
fabricated node), `UNWIND` batches with uid anchors, scope+evidence-scoped
retract (`DETACH DELETE` on reducer-owned `SecretsIAM*` nodes only; never on
`CloudResource`/`KubernetesWorkload`), and `WrapRetryableNeo4jError` idempotent
dispatch. There is no hot-path query or graph behavior to regress. Correctness is
covered by `go test ./internal/reducer -run Extract` (exact-only admission, all
five non-exact states, missing-endpoint skip+count, IAM-role-unresolved skip,
duplicate-delivery idempotency, empty, tombstone/foreign-kind, JSON `[]any` wire
format, redaction allowlist), `go test ./internal/storage/cypher -run
SecretsIAMGraph` (node/edge Cypher shape, uid-only MERGE identity, scoped
retract with no endpoint deletion, batching), and `go test ./internal/graph`
(schema statements). The §12 writer benchmark on the shipped node/edge writer
shape and the NornicDB/Neo4j conformance proof land with the reducer-domain PR
that executes the writer.

No-Observability-Change: this step adds no metrics, spans, logs, or status
fields. The graph-projection telemetry (nodes/edges written, skipped+reason,
phase durations, readiness-block reason) lands with the reducer-domain writer
PR, where there is a runtime path to observe.
