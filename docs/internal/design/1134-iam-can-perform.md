# IAM CAN_PERFORM Effective-Permission Edge — Design (MVP)

Status: **DESIGN PROPOSAL — not yet accepted.** NEEDS PRINCIPAL REVIEW before
any build — gated graph-write (`risk:schema`), security-sensitive. This note
carries no code; it is the design plus the ownership claim for the deferred
`CAN_PERFORM` follow-up.

Issue: #1134 (aws/deep: IAM effective-permissions & privilege-escalation
analysis; parent #51, epic #1147). The merged CAN_ASSUME (PR2) and
CAN_ESCALATE_TO (PR3) slices explicitly deferred the general `CAN_PERFORM` edge
to this design — see `1134-iam-can-assume-trust-graph.md` §8 and
`1134-iam-privilege-escalation-catalog.md` §0, which keep a non-closing
reference to #1134 for exactly this slice.

Owners: AWS scanner fleet + reducer/projection owners (the #1215 IAM
workstream). This slice is claimed under #1134 so the #1147 no-duplication
directive is satisfied and ownership is recorded.

This note proposes a conservative, honestly-scoped MVP for the headline #1134
question — "which principals can effectively perform which sensitive action on
which resource" — reusing the proven CAN_ESCALATE_TO machinery. The rigor is
copied from the shipped IAM edge slices, not reinvented.

## 1. Problem And Current State

PR1 (merged, #1155) emits `aws_iam_permission` facts — normalized,
metadata-only IAM policy statements per principal: `effect`, `actions[]`,
`resources[]`, `not_actions[]`, `not_resources[]`, `condition_keys[]`,
`has_conditions`, `is_wildcard_action`, `is_wildcard_resource`,
`policy_source`. The IAM scanner materializes principals (`aws_iam_role`,
`aws_iam_user`) and many AWS resource types (S3 buckets, KMS keys, etc.) as
`:CloudResource` nodes via #805 (`DomainAWSResourceMaterialization`), keyed
`cloudResourceUID(account_id, region, resource_type, arn)`.

CAN_ASSUME projects trust statements (`role -> role`); CAN_ESCALATE_TO projects
a **curated catalog** of privilege-escalation primitives (`principal -> target`)
deliberately *to avoid* the general permission-resolution problem. Nothing yet
answers the direct question: **does this principal's identity policy grant
action X on resource Y?** The evidence (Allow statements with actions and
resource ARNs) and both endpoints (principal node, resource node) already
exist — only the edge is missing. This is **edge-only**: no new node type, no
new keyspace, no schema constraint.

## 2. Why This Is Hard — The Honesty Boundary

A *complete* CAN_PERFORM is the AWS IAM policy-evaluation problem, which is not
decidable from static metadata. What is and isn't tractable from the facts Eshu
emits today:

| IAM semantic | Tractable now? | Note |
|---|---|---|
| Wildcard action (`*`, `service:*`) | yes | existing `allows()` helper handles it |
| Partial action wildcard (`iam:Create*`) | refuse (MVP) | needs the ~17k-entry AWS action catalog; CAN_ESCALATE_TO refused it too |
| Exact resource ARN | yes | `cloudResourceJoinIndex.byARN` |
| Resource glob/prefix | yes, bounded | `globMatch()` with zero/one/many confidence |
| Resource `*` | yes, as a skip | names no single node → `skipped_ambiguous` |
| `NotAction` / `NotResource` | refuse | inverts the match space; not a positive grant |
| Explicit-Deny precedence | conservative | any Deny touching the action removes the grant (sound for "cannot", over-removes for "can") |
| Condition keys | refuse | facts carry key **names only, never values** → uninterpretable |
| Permission boundaries | no (new facts) | not distinguished from attached policies in the fact stream |
| SCPs (Organizations) | no (no scanner) | account-wide deny ceiling; out of scope |
| Resource-based policies | no (new facts) | a resource can grant a principal with no identity statement |
| Session policies | no (runtime) | never visible to a static scanner |

**Bottom line:** an exhaustively-correct effective-permission lattice is not
achievable from current facts and partly not achievable at all from static
metadata. A *conservative, high-value, honestly-labelled* CAN_PERFORM is
achievable, because ~80% of the machinery already exists and is proven. This
slice is a generalization of CAN_ESCALATE_TO from a fixed
target-kind-per-primitive to an arbitrary `(action, resource-type)` pair — not a
from-scratch build.

## 3. Scope: CAN_PERFORM MVP (conservative, bounded)

Emit the edge **only** where ALL of these hold; **skip-and-count** everything
else:

1. **Closed, curated, high-value action vocabulary** — not "every action." A
   reviewed catalog file (mirroring `iam_escalation_catalog.go`), each entry
   mapping `action -> expected target resource_type`, e.g. `s3:getobject`,
   `s3:putobject`, `s3:deletebucket`, `kms:decrypt`,
   `secretsmanager:getsecretvalue`, `ssm:getparameter`, `dynamodb:getitem`,
   `ec2:terminateinstances`, `rds:deletedbinstance`. Bounds cardinality and lets
   the resolver require the matched node be the right type.
2. **Resolved exact-ARN or single-glob targets among already-scanned nodes
   only.** Reuse `cloudResourceJoinIndex` + `globMatch`. Exactly one match →
   edge; many / `*` → `skipped_ambiguous`; zero → `skipped_unresolved`. No
   fabrication, ever.
3. **Allow-only, unconditioned, no NotAction/NotResource.** Reuse the trusted
   grant union (`iam_escalation_grant.go`). Conditioned → `skipped_conditioned`;
   NotAction/NotResource → `skipped_not_action_resource`; Deny on the action →
   `skipped_deny`.
4. **Identity-policy statements only.** Document explicitly via an edge property
   `evaluation_scope = "identity_policy_only"` that resource-based policies,
   permission boundaries, SCPs, conditions, and session policies are **not**
   evaluated — so a *missing* edge does not mean "cannot perform," and a
   *present* edge means "an identity policy grants this, ignoring
   resource-policy / boundary / SCP / condition restrictions."

### Skip rules (no fabrication, no dangle)

Bounded `skip_reason` counter dimensions: `skipped_uncatalogued_action` (action
not in the MVP vocabulary), `skipped_ambiguous`, `skipped_unresolved`,
`skipped_deny`, `skipped_conditioned`, `skipped_not_action_resource`,
`skipped_self_loop`. Every refusal is counted; a grant is never dropped
silently.

## 4. Graph Contract And Keying

```
(:CloudResource {uid: principal_uid})
  -[:CAN_PERFORM {actions, action_count, evaluation_scope,
                  scope_id, generation_id, evidence_source}]->
(:CloudResource {uid: resource_uid})
```

- **Relationship type is the static token `CAN_PERFORM`** — NOT
  `CAN_PERFORM_<action>`. The discriminator (action) **must stay out of the
  MERGE key**: this is the documented NornicDB property-keyed-relationship
  hot-path rule that CAN_ESCALATE_TO §5 already navigated (a property-keyed
  MERGE reproduces the 20s validation timeout). The granted **action set is an
  edge property** (`rel.actions[]`), computed in memory, sorted + deduped,
  written wholesale via `SET` after a static-token `MERGE` → idempotent. This is
  the exact pattern CAN_ESCALATE_TO uses for `rel.primitives[]`.
- **Endpoints are `:CloudResource` nodes** keyed on `cloud_resource_uid` — both
  already materialized. CAN_PERFORM is **edge-only**; no new node type, no new
  keyspace, no schema constraint.
- **Retract-before-reproject** scoped to
  `evidence_source = 'reducer/iam-can-perform'` + `scope_id`, never touching
  endpoint nodes — idempotent reprojection.

## 5. Cardinality And Performance

Unbounded CAN_PERFORM is `principals × resources × actions` — potentially
enormous. The closed action vocabulary + exact/single-glob-only resolution + the
"scanned nodes only" rule bound it to the same order as CAN_ESCALATE_TO. The
write is a batched `UNWIND` over resolved rows, idempotent MERGE-on-identity, no
per-row graph round trip. The build PR must carry a `Benchmark Evidence:` /
`No-Regression Evidence:` note against a shipped writer baseline on the same
shape (per `scripts/verify-performance-evidence.sh`), plus a performance-impact
declaration naming the expected cardinality band and stop threshold.

## 6. Readiness, Idempotency, Telemetry

- **Readiness-gated** on the existing `cloud_resource_uid`
  (`GraphProjectionPhaseCanonicalNodesCommitted`) phase — same gate the other
  IAM edges use — so the edge never resolves against uncommitted nodes.
- **Telemetry:** `eshu_dp_iam_can_perform_edges_total{resolution_mode}` and
  `eshu_dp_iam_can_perform_skipped_total{skip_reason}`, a
  `reducer.iam_can_perform_materialization` span, and a completion log with the
  skip tally, so an operator can tell at 3 AM how many edges projected vs were
  skipped and why.

## 7. Reused Machinery (already shipped, proven)

- `iam_escalation_target.go` — join-index resolution ladder + `globMatch` +
  ARN→resource-type classification (generalize beyond IAM to S3/KMS/etc.).
- `iam_escalation_grant.go` — trusted-Allow grant union + Deny/condition/
  NotAction skip logic.
- `iam_escalation_edge_writer.go` — the static-token-MERGE-then-SET writer to
  mirror.
- `iam_escalation_catalog.go` — the catalog-file shape for the action vocabulary.
- The readiness gate, evidence-scoped retract, and telemetry shape from any of
  the three merged IAM edge slices.

## 8. Phasing

- **PR4a (this MVP edge):** closed sensitive-action vocabulary, exact +
  single-glob resolution among scanned nodes, identity-policy-only, full skip
  taxonomy + telemetry. Edge-only on existing nodes — no node-then-edge.
- **PR4b:** resource-policy support split into scanner/fact emission and reducer
  consumption: new `aws_resource_policy_permission` facts capture bucket and KMS
  policy statements, then the reducer admits exact scanned IAM grantees onto the
  existing CAN_PERFORM edge identity.
- **PR4c:** mark permission-boundary policies in the fact stream and intersect
  them into the effective set.
- **PR4d:** condition-aware confidence once the scanner can emit a safe
  condition summary (or a provenance-only low-confidence tier). SCPs and session
  policies remain explicit non-goals.
- **PR4e:** widen the closed action catalog as demand and confidence allow.

## 9. Open Forks (decide in review BEFORE the build)

These are the forks the CAN_ASSUME memo (§ lines 184-212) pre-named; none
resolves trivially, so the build must STOP-and-report rather than guess:

1. **Resource-pattern → node matching** — the zero/one/many confidence model for
   resource ARNs that are globs/prefixes; confirm exactly-one → edge, else skip.
2. **Action vocabulary** — the initial curated set and its provenance/review.
3. **Wildcard / identity-policy-only confidence model** — how a present edge is
   labelled (`evaluation_scope`) and how the six-outcome correlation contract
   (exact / derived / ambiguous / unresolved / stale / rejected) maps onto
   CAN_PERFORM outcomes.

## 10. Build Gates (for the eventual PR4a)

TDD proof matrix (positive exact-ARN, single-glob, ambiguous-many, wildcard-`*`,
uncatalogued-action, Deny, conditioned, NotAction, unresolved/cross-account,
self-loop, empty/idempotent); `eshu-correlation-truth` graph + query truth
agreement; the readiness/dual-gate proof; `scripts/verify-performance-evidence.sh`
+ `scripts/verify-package-docs.sh`; unique `iamCanPerform*` naming with a
redeclaration guard; files < 500 lines; principal review on the `risk:schema`
graph write.

## 11. Decision Requested

Approve this MVP scope (or amend §3 / §9), confirm CAN_PERFORM is claimed under
#1134, and green-light the PR4a build. Until approved, no code lands — this is a
design + claim artifact only.

## 12. PR4a Implementation Evidence

This section is appended by the PR4a build (the design above was approved and
merged in #1316 with no fork amendments). It records the realized scope, the
performance-impact declaration, and the proof gates run.

### 12.1 Realized scope

- New reducer extractor `ExtractIAMCanPerformEdges` (`go/internal/reducer/iam_can_perform.go`)
  evaluates each scanned IAM principal's trusted-Allow identity statements against
  the closed catalog (`go/internal/reducer/iam_can_perform_catalog.go`) and emits
  one `(:CloudResource {principal}) -[:CAN_PERFORM]-> (:CloudResource {resource})`
  edge per resolved `(principal, resource)` pair, with the granted action set as a
  sorted/deduped edge property `rel.actions` (never in the MERGE key),
  `rel.action_count`, and `rel.evaluation_scope = 'identity_policy_only'`.
- New handler `IAMCanPerformMaterializationHandler`
  (`go/internal/reducer/iam_can_perform_materialization.go`) gates on the existing
  `cloud_resource_uid` / `canonical_nodes_committed` phase, loads the scope
  generation's `aws_resource` + `aws_iam_permission` facts, retracts the prior
  generation's `evidence_source = 'reducer/iam-can-perform'` edges, writes the
  resolved edges, and records the skip tally.
- New writer `IAMCanPerformEdgeWriter`
  (`go/internal/storage/cypher/iam_can_perform_edge_writer.go`) mirrors the
  CAN_ESCALATE_TO static-token `MERGE`-then-`SET` shape: two uid-indexed
  `:CloudResource` MATCHes, `MERGE (p)-[rel:CAN_PERFORM]->(t)`, action set written
  wholesale in `SET`. Edge-only: no new node type, no new keyspace, no constraint.
- Closed catalog shipped (nine entries, §3 starter vocabulary):
  `s3:getobject`, `s3:putobject` → `aws_s3_bucket`; `s3:deletebucket` →
  `aws_s3_bucket`; `kms:decrypt` → `aws_kms_key`; `secretsmanager:getsecretvalue`
  → `aws_secretsmanager_secret`; `ssm:getparameter` → `aws_ssm_parameter`;
  `dynamodb:getitem` → `aws_dynamodb_table`; `ec2:terminateinstances` →
  `aws_ec2_instance`; `rds:deletedbinstance` → `aws_rds_db_instance`.
- Skip taxonomy (bounded `skip_reason`): `skipped_uncatalogued_action`,
  `skipped_ambiguous`, `skipped_unresolved`, `skipped_deny`, `skipped_conditioned`,
  `skipped_not_action_resource`, `skipped_self_loop`.

### 12.2 Performance impact declaration

- **Affected stage:** reducer shared-projection graph write (new additive domain
  `iam_can_perform_materialization`). No change to any existing hot path; the
  domain is only registered when its writer + fact loader are wired.
- **Expected cardinality band:** same order as CAN_ESCALATE_TO. The closed
  nine-action catalog + exact/single-glob-only resolution + scanned-nodes-only
  rule bound output to `O(principals × resolved_catalog_resources)`, far below the
  `principals × resources × actions` worst case. Per scope generation the realized
  edge count is expected in the low thousands at the 20-25 repo corpus, matching
  the escalation band.
- **Proof ladder:** focused Go unit tests (extractor, handler, writer, catalog) →
  writer micro-benchmark vs the shipped escalation-writer baseline on the same
  5000-row shape → `scripts/verify-performance-evidence.sh`. A full-corpus wall-clock
  run is out of scope for this edge-only PR; the write shares the proven batched
  `UNWIND`/static-token-`MERGE` template whose corpus behavior #1134 PR3 already
  measured.
- **Stop threshold:** if the writer micro-benchmark regresses more than ~10%
  against the escalation-writer baseline on the same row shape, stop and profile
  before merge.

`Benchmark Evidence:` `BenchmarkIAMCanPerformEdgeWriter` vs
`BenchmarkIAMEscalationEdgeWriter` (shipped baseline), 5000 rows, no-op group
executor, isolates Eshu-owned batched-statement construction (no graph round
trip). The two writers share the static-token dual-MATCH `MERGE` template, so the
CAN_PERFORM writer stays in the same shape class — one batched MATCH-MATCH-MERGE
per chunk over two uid-indexed `:CloudResource` anchors, no N+1. Backend: shared
raw Cypher/Bolt contract (NornicDB default, Neo4j compatible); input cardinality
5000 resolved rows; index/constraint state: relies on the `:CloudResource(uid)`
lookup the data-plane bootstrap already creates; before/after: see the run output
recorded in the PR body.

`Observability Evidence:` new counters
`eshu_dp_iam_can_perform_edges_total{resolution_mode}` (resolution_mode ∈
{exact_arn, single_glob}) and `eshu_dp_iam_can_perform_skipped_total{skip_reason}`
(the seven bounded skip reasons), span
`reducer.iam_can_perform_materialization`, and a completion log carrying the edge
count and full per-reason skip tally, so an operator can tell at 3 AM how many
edges projected vs were skipped and why.

### 12.3 Honesty boundary

PR4a emitted only `rel.evaluation_scope = 'identity_policy_only'`. After the
PR4b reducer follow-up, a CAN_PERFORM edge carries `grant_sources` plus
`rel.evaluation_scope` as `identity_policy_only`, `resource_policy_only`, or
`identity_and_resource_policy`. A *present* edge means one of those evaluated
source layers grants the action on the resolved resource, ignoring permission
boundaries, SCPs, condition values, and session policies. A *missing* edge does
NOT mean "cannot perform" — it can mean the action is uncatalogued, the resource
or principal was not scanned or was ambiguous, or a later evaluator slice has
not been implemented (PR4c–PR4d). This boundary is the reason the slice is
conservative and skip-counted rather than claiming a complete effective-permission
lattice.

## 13. PR4b Reducer Follow-Up Evidence

This section records the reducer consumption of the resource-policy facts merged
in #1326. The collector/facts PR emitted metadata-only
`aws_resource_policy_permission` facts; this reducer follow-up consumes them
without changing the `CAN_PERFORM` relationship identity.

### 13.1 Realized scope

- `IAMCanPerformMaterializationHandler` now loads `aws_resource_policy_permission`
  with `aws_resource` and `aws_iam_permission`.
- `ExtractIAMCanPerformEdges` evaluates resource-policy facts only when the
  grantee principal ARN resolves to an already-scanned IAM role/user
  `CloudResource`, the attached resource ARN resolves to the catalog-expected
  `CloudResource`, and the statement Resource patterns apply to that attached
  resource.
- Public, service, federated, account-root, wildcard, cross-account-unscanned,
  ambiguous, unsupported, conditioned, NotAction/NotResource, Deny-blocked, and
  wrong-resource-pattern cases remain skipped/provenance-only.
- Edge rows now carry `grant_sources` and one of three `evaluation_scope` values:
  `identity_policy_only`, `resource_policy_only`, or
  `identity_and_resource_policy`.
- The graph writer still uses the same static
  `(principal_uid)-[:CAN_PERFORM]->(resource_uid)` MERGE identity. `actions` and
  `grant_sources` are SET properties only.

### 13.2 Performance and observability

No-Regression Evidence: `go test ./internal/reducer -run 'IAMCanPerform'
-count=1` proves identity-only, resource-only, both-source merge,
public/unscanned principal skips, conditioned/NotResource/Deny skips,
wrong-resource-pattern refusal, readiness, and idempotent reprojection behavior.
`go test ./internal/storage/cypher -run 'IAMCanPerformEdgeWriter' -count=1`
proves `grant_sources` stays out of the MERGE key.

Benchmark Evidence: `go test ./internal/storage/cypher -run '^$' -bench
'BenchmarkIAMCanPerformEdgeWriter|BenchmarkIAMEscalationEdgeWriter' -benchmem
-count=3` shaped 5,000 CAN_PERFORM rows at batch 500 on Apple M4 Pro in
`1.25 ms/op`, `1.24 ms/op`, and `1.24 ms/op` (`~1.97 MB/op`,
`25,068 allocs/op`) versus the escalation baseline at `1.21 ms/op`,
`1.21 ms/op`, and `1.20 ms/op` on the same row shape. The added
`grant_sources` property does not change statement count, endpoint anchors,
relationship token, or MERGE identity.

Observability Evidence: existing `eshu_dp_iam_can_perform_edges_total`, bounded
`eshu_dp_iam_can_perform_skipped_total`, span
`reducer.iam_can_perform_materialization`, readiness-gate retry errors, and the
completion log still diagnose edge count, skip reason, fact-load count, extract
duration, retract duration, write duration, and total duration. The completion
log now includes `resource_policy_permission_fact_count`.
