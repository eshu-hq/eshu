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
- **PR4b:** new `aws_resource_policy_permission` scanner facts so the resource
  side (bucket policy, KMS key policy) contributes grants — closes the largest
  blind spot.
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
