# IAM CAN_ASSUME Trust-Graph Edge Materialization Design

Status: design accepted for the CAN_ASSUME slice (this PR2). The escalation
edges (`CAN_PERFORM`, `CAN_ESCALATE_TO`) remain a follow-up design fork — see
§8. NEEDS PRINCIPAL REVIEW — gated graph-write (`risk:schema`).

Issue: #1134 (aws/deep: IAM effective-permissions & privilege-escalation
analysis; parent #51). PR1 (merged) emits the `aws_iam_permission` fact. This
PR2 projects only the trust-graph (`CAN_ASSUME`) slice.

Owners: AWS scanner fleet + reducer/projection owners.

This note is the durable design for projecting the derived `aws_iam_permission`
trust statements into canonical `(:CloudResource)-[:CAN_ASSUME]->(:CloudResource)`
graph edges. It mirrors the shipped #805 AWS relationship edge materializer and
the #391 PR3 / #1135 PR2b closed-vocabulary edge writers; the rigor is copied,
not reinvented.

## 1. Problem And Current State

PR1 emits `aws_iam_permission` facts — normalized, metadata-only IAM policy
statements per principal. A `policy_source = trust` fact captures the role's
trust policy: `principal_arn` is the role-with-trust-policy ARN, and
`assume_principals[]` lists the principals that role grants assume-role to. The
IAM scanner also emits IAM roles and users as `aws_resource` facts, which #805
PR1 (`DomainAWSResourceMaterialization`) materializes as `:CloudResource` nodes
keyed `cloudResourceUID(account_id, region, "aws_iam_role"|"aws_iam_user",
arn)`.

Today nothing turns the trust statements into a queryable assume-role graph.
The security question "which principals can assume which roles" has the
evidence (`assume_principals`) and both endpoints already exist as nodes — only
the edge is missing. This is **edge-only**: no new node type, no new keyspace,
no schema constraint (the `CloudResource` uid constraint already exists in
`go/internal/graph/schema.go`).

## 2. Scope: CAN_ASSUME Only

For each `aws_iam_permission` fact with `policy_source = trust` and
`effect = Allow`, for each ARN in `assume_principals` that resolves to a scanned
IAM `:CloudResource` node, MERGE:

```
(:CloudResource {uid: assuming_principal_uid})
  -[:CAN_ASSUME {scope_id, generation_id, evidence_source}]->
(:CloudResource {uid: role_with_trust_policy_uid})
```

The role-with-trust-policy is the fact's `principal_arn`; the assuming principal
is each entry in `assume_principals`.

### Skip rules (no fabrication, no dangle)

An assume-principal is **skipped and counted**, never written, when it:

- is `*` or contains `*` (wildcard principal — public/anonymous trust),
- is not an ARN (an AWS-service principal like `ec2.amazonaws.com`,
  a SAML/OIDC federated provider ARN, a canonical-user id, or a bare account
  id `123456789012`),
- is an account-root ARN (`arn:aws:iam::<acct>:root`) — account-level, not a
  scanned role/user node,
- is an ARN that does not resolve to a `:CloudResource` node scanned in this
  scope generation (cross-account/region trust whose target account was not
  scanned — the #805 trust-boundary rule).

A `Deny` trust statement, or a fact whose own `principal_arn` does not resolve
to a scanned role node, materializes no edge and is counted. A self-assume
(`assuming_principal_uid == role_uid`) is skipped without counting it as
unresolved (both endpoints resolved; the loop carries no trust truth) — mirrors
the #805 self-loop rule.

## 3. Resolution: In-Memory Join Index

Resolution reuses the bounded in-memory join model from #805
(`cloudResourceJoinIndex`): build a `byARN` map from the scope generation's
`aws_resource` facts once, then resolve each assume-principal ARN and the fact's
own `principal_arn` by O(1) ARN lookup. No per-edge graph round trip, no N+1
Cypher. An ARN that is not in the index did not scan as a node, so it produces
no edge — graceful degradation, counted as `unresolved`.

Because each index entry is derived from an `aws_resource` fact that carried its
own `account_id`/`region`, a cross-account assume-principal resolves only if
that account's role/user was scanned in the same scope — the trust-boundary
rule, never fabricated.

## 4. Closed-Vocabulary Static-Token Edge

`CAN_ASSUME` is a closed single-member relationship vocabulary. The cypher
writer interpolates the validated static token into the relationship-type
position (which cannot be parameterized) only after the character-class +
allowlist screen, exactly like the #391 PR3 `AWS_COVERS_<signal>` and #1135
PR2b `ALLOWS_INGRESS/EGRESS` writers. Two `MATCH (:CloudResource {uid})` clauses
precede the `MERGE` so a missing endpoint is a no-op, never a fabricated node.

Edge identity is `(principal_uid, CAN_ASSUME, role_uid)`; the `MERGE` is on that
identity only, mutable `scope_id`/`generation_id`/`evidence_source` are `SET`
separately. Idempotent under retries, duplicate facts, and reprojection.

## 5. Readiness Gate And Concurrency

Both endpoints are `:CloudResource` nodes published under the existing
`cloud_resource_uid` / `canonical_nodes_committed` phase (#805 PR1). The new
`DomainIAMCanAssumeMaterialization` gates on that exact phase, identically to
`DomainAWSRelationshipMaterialization` and
`DomainObservabilityCoverageMaterialization`:

- handler-side: `canonicalNodesReady` returns a retryable
  `iamCanAssumeNodesNotReadyError` when the phase is not published, so the
  durable queue re-runs the intent once nodes commit;
- queue-side: the domain is added to the existing `cloud_resource_uid` readiness
  clause in `claimReducerWorkQuery`, `reducer_queue_batch.go` (both the eligible
  predicate and the same-conflict-key tiebreak), and `status_blockage.go`.

Conflict domain / key: the intent is anchored to the same
`aws_resource_materialization:<scope>` entity key as the #805 edge intent, so
the readiness slice matches the published phase row, and the prior-generation
retract is evidence-scoped to `evidence_source = reducer/iam-can-assume`.

**Concurrency posture.** The write is idempotent by edge identity, partitioned
by scope conflict key, and uses no serialization workaround. "Serialization Is
Not A Fix" holds: the `MERGE` converges under concurrent reprojection without
reducing workers, batch size, or writer concurrency. Duplicate `assume_principals`
entries and duplicate trust facts dedupe to one edge in the extractor's `seen`
set before the write.

## 6. Telemetry

- Counter `eshu_dp_iam_can_assume_edges_total`, labels
  `principal_kind` (role / user — the resolved assuming-principal node type) and
  `resolution_mode` (arn / unresolved). Bounded cardinality.
- Span `reducer.iam_can_assume_materialization` wraps fact-load, join-index
  build, resolution, retract, and the batched MATCH-MATCH-MERGE write.
- Completion log carries fact count, edge count, and the bounded skip tally by
  reason (wildcard / service_or_account / external_unresolved / deny /
  source_unresolved / self_assume) so an operator can answer "which trust
  principals are losing edges, and is it because the target account was not
  scanned?" at 3 AM without a per-edge log line.

## 7. Performance Impact Declaration

- **Stage:** reducer shared projection, one intent per scope generation that has
  `aws_iam_permission` trust facts.
- **Cardinality:** trust facts per scope (one per role trust statement) times
  the small `assume_principals` fan-out per statement. Join index is the scope's
  `aws_resource` fact count. All in-memory and bounded; no per-edge graph round
  trip.
- **Hot path:** the batched `UNWIND $rows MATCH-MATCH-MERGE` edge write, anchored
  on the `CloudResource.uid` uniqueness constraint at both `MATCH` sites —
  identical shape to #805 / #391 PR3 / #1135 PR2b, which are within the
  performance contract. Relationship type is a static token (not a relationship-
  property `MERGE`, which timed out at 20s on NornicDB per #805 §5.3).
- **Proof ladder:** focused reducer extractor + handler tests, cypher writer
  static-token tests, postgres readiness-gate query tests. No new query shape
  vs. the shipped edge writers, so a no-regression argument on the same write
  shape stands in for a fresh full-corpus bench in this slice.
- **Stop threshold:** if a corpus run shows the IAM edge write exceeding the
  #805 edge write per-row time by more than ~10%, profile before merge.

No-Regression Evidence: the CAN_ASSUME edge write reuses the shipped
closed-vocab static-token `UNWIND ... MATCH (:CloudResource {uid}) MATCH
(:CloudResource {uid}) MERGE (a)-[:CAN_ASSUME]->(b)` shape (NornicDB + Neo4j,
`CloudResource.uid` uniqueness constraint present from #805 PR1), which #805
(`aws_relationship_edges`), #391 PR3 (`AWS_COVERS_<signal>`), and #1135 PR2b
(`ALLOWS_INGRESS/EGRESS`, `TO`) already measured within the repo-scale edge
write contract. Input shape is the scope's trust-fact fan-out, resolved O(1)
in-memory with no per-edge round trip, so this slice adds no new hot-path Cypher
shape and no per-edge graph lookup. Cardinality at the anchor: both `MATCH`
sites hit the `CloudResource.uid` constraint index; batch size is the shared
`DefaultBatchSize` (500).

Observability Evidence: `eshu_dp_iam_can_assume_edges_total{principal_kind,
resolution_mode}` counter, the `reducer.iam_can_assume_materialization` span,
and the "iam can-assume materialization completed" structured completion log
(fact count, edge count, skip-reason tally) let an operator see materialized vs.
skipped edges and the reason class for skips. The readiness gate surfaces a
`readiness` conflict-domain blockage row in `status_blockage.go` when canonical
nodes have not committed.

## 8. Follow-Up Design Fork: CAN_PERFORM And CAN_ESCALATE_TO (NOT in this PR)

These are deferred because they require resolution machinery this slice
deliberately does not build. STOP-and-report rather than guess:

1. **`CAN_PERFORM {action}` over the escalation-primitive action set.** The
   `aws_iam_permission` identity-policy facts carry `actions[]` and `resources[]`
   that are *patterns* (`arn:aws:iam::*:role/*`, `s3:GetObject` on
   `arn:aws:s3:::sensitive-*`). Materializing `(:principal)-[:CAN_PERFORM]->
   (:resource)` needs a **resource-pattern → node matching strategy**: how a
   wildcard/partial ARN pattern matches the set of scanned `CloudResource` nodes
   (prefix/glob expansion, account/region scoping, and the confidence model when
   a pattern matches zero, one, or many nodes). That is a real design fork, not a
   trivial ARN equality lookup, so it is out of scope here.

2. **The escalation-primitive action set.** The curated ~20 documented IAM
   privilege-escalation primitives (`iam:PassRole`, `iam:CreatePolicyVersion`,
   `iam:AttachUserPolicy`, `lambda:CreateFunction`+`PassRole`, …) need a
   catalog with the action-combination semantics (some primitives require two
   actions together). This catalog and its provenance need their own review.

3. **`CAN_ESCALATE_TO {pattern}`** composes (1) and (2) into a derived
   escalation edge between principals. It depends on both above plus a
   **confidence model for wildcard actions/resources** (an `iam:*` on `*` is a
   different confidence than a named action on a named resource). The
   six-outcome correlation contract (exact/derived/ambiguous/…) must classify
   each escalation edge before promotion.

What is needed to unblock the follow-up, in order: (a) the resource-pattern →
node matching strategy with its zero/one/many confidence model; (b) the
escalation-primitive action catalog with multi-action combination semantics;
(c) the wildcard confidence model that decides which derived escalation edges
promote vs. stay provenance-only. None resolves trivially, so none is attempted
in this slice.
