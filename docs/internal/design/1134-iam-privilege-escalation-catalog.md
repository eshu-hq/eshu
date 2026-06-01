# IAM Privilege-Escalation Primitive Catalog — Design + Review Focus

Status: **gated graph-write (`risk:schema`), security-sensitive. NEEDS PRINCIPAL
REVIEW. Do not merge without principal review.**
Issue: #1134 (aws/deep: IAM effective-permissions & privilege-escalation
analysis). This memo covers **PR3**: the `CAN_ESCALATE_TO` edge.
Owners (proposed): reducer/projection owners + AWS scanner fleet + a security
reviewer for the catalog and confidence model.

This note lives under `docs/internal/design/` because it is maintainer design,
not operator-facing reference (`docs/mkdocs.yml` sets `docs_dir: public`, so this
file is intentionally outside the strict mkdocs build — same placement as the
#391 and #805 design memos).

**This catalog and the conservative confidence model below are the review
focus.** Everything downstream (the reducer extractor, the Cypher edge writer,
the telemetry) is a faithful mirror of the already-shipped network-reachability
edge slice (#1135 PR2b) and correlation edge slice (#388 PR3). The novel,
security-load-bearing decisions are: *which* action combinations we treat as an
escalation primitive, *what* each escalates to, and *when* we refuse to promote a
finding to an edge.

---

## 0. Scope and non-goals

In scope (PR3):

- A **curated, documented** catalog of well-known IAM privilege-escalation
  primitives. Each entry names the IAM action(s) it requires and the target it
  escalates to.
- A single closed-vocabulary relationship type
  `(:CloudResource principal)-[:CAN_ESCALATE_TO]->(:CloudResource target)`,
  written only when the principal holds a **complete** primitive (all actions
  present, `Allow`, not `Deny`) **and** the target resolves to exactly one
  scanned IAM `:CloudResource` node.

Explicitly **out of scope** (named follow-ups):

- The general `(:principal)-[:CAN_PERFORM {action}]->(:resource)` edge — any
  action on any resource. That is the full resource-pattern / effective-
  permission lattice problem and is deferred (issue #1134 keeps a non-closing
  reference).
- The `CAN_ASSUME` trust edge. That is a **separate, already-designed slice**
  (#1134 PR2, PR #1214). The `sts:AssumeRole` escalation primitive **defers** to
  that edge and emits **nothing** here, by design, to avoid a duplicate
  relationship for the same real-world fact (see §3, `sts:AssumeRole`).
- Full IAM condition evaluation (IP / MFA / tag / `iam:PassedToService`). PR1
  emits only a condition-key **summary** (`condition_keys[]`, `has_conditions`),
  never condition values, so this slice cannot conservatively satisfy a
  condition. **Any statement with `has_conditions = true` is skipped** (§4).
- Transitive escalation chains (escalate-to-A-then-A-escalates-to-B). The edge is
  single-hop; transitivity is a graph-query concern, not a write concern.

---

## 1. Input contract (what PR1 already emits)

The reducer consumes the merged `aws_iam_permission` fact (kind
`facts.AWSIAMPermissionFactKind`, schema `1.0.0`). Each fact is one normalized
IAM policy statement attached to a principal. Relevant payload fields
(`go/internal/collector/awscloud/iam_permission_envelope.go`):

| Field | Meaning | Normalization |
| --- | --- | --- |
| `account_id`, `region` | AWS boundary | verbatim |
| `principal_arn` | the principal the statement is attached to | verbatim ARN |
| `principal_type` | role / user / group | verbatim |
| `policy_source` | `inline` / `attached_managed` / `trust` | verbatim |
| `effect` | `Allow` or `Deny` | canonicalized |
| `actions[]` | granted actions | **lowercased**, de-duped, sorted |
| `not_actions[]` | `NotAction` set | lowercased, de-duped, sorted |
| `resources[]` | resource ARN patterns | verbatim (case-sensitive), sorted |
| `not_resources[]` | `NotResource` patterns | verbatim, sorted |
| `condition_keys[]` | condition **key names only** (no values) | verbatim, sorted |
| `assume_principals[]` | trust-policy principals | verbatim, sorted |
| `has_conditions` | `len(condition_keys) > 0` | derived |
| `is_wildcard_action` | `actions` contains `*` | derived |
| `is_wildcard_resource` | `resources` contains `*` | derived |

Because `actions[]` is **lowercased**, the catalog stores every action token in
lowercase (`iam:createpolicyversion`, not `iam:CreatePolicyVersion`). IAM actions
are case-insensitive on the AWS side, so this is loss-free; matching lowercase
against lowercase is exact and avoids a per-statement re-fold.

A principal's *effective* grant for a primitive is the **union of its Allow
statements**: PR1 emits one fact per statement, so a principal can satisfy a
multi-action primitive across several statements. The extractor unions a
principal's qualifying Allow statements before deciding a primitive is complete
(§4).

---

## 2. Confidence / resolution model (the conservative contract)

The whole point of this slice is to be **conservative**: a false escalation edge
is worse than a missing one, because it would mislead a security team about blast
radius. The rules, in order:

1. **Effect.** Only `Allow` statements feed a primitive. Every `Deny` statement
   is skipped. We do **not** attempt Deny-overrides-Allow evaluation in this
   slice (that needs full policy evaluation); instead, **any principal that has a
   `Deny` touching a primitive's action set is removed from that primitive
   entirely** — a conservative under-approximation (we would rather drop a real
   edge than emit one a Deny actually blocks). Recorded as `skipped_deny`.

2. **Conditions.** Any statement with `has_conditions = true` cannot be
   conservatively evaluated (PR1 carries key names, not values). A conditioned
   statement is **not** counted toward a primitive. If dropping it leaves the
   primitive incomplete, the primitive is skipped and counted
   `skipped_conditioned`. (An unconditioned statement that already completes the
   primitive still produces the edge; a *separate* conditioned statement does not
   block it.)

3. **`NotAction` / `NotResource`.** A statement that uses `not_actions` or
   `not_resources` inverts the match space and cannot be conservatively reduced
   to "grants action X on resource Y." Such statements are **not** counted toward
   a primitive (recorded `skipped_not_action_resource`). They are not treated as
   wildcards; they simply do not contribute a positive grant we can trust.

4. **Action presence (multi-action AND).** A primitive lists one or more required
   actions. The primitive is *armed* only if **every** required action is present
   in the principal's unioned trusted-Allow action set. An action is "present" if
   the exact lowercase token is in `actions[]`, **or** a service-or-global
   wildcard the principal holds covers it (`*`, or a `service:*` prefix matching
   the action's service — e.g. `iam:*` covers `iam:createpolicyversion`). Partial
   wildcards inside an action (`iam:Create*`) are **not** expanded in this slice
   (conservative: we only honor the two unambiguous wildcard shapes `*` and
   `service:*`). A primitive missing any action is recorded `skipped_incomplete`.

5. **Target resolution.** Each armed primitive names a **target identity** drawn
   from the statement's `resources[]` (e.g. the policy ARN for
   `iam:CreatePolicyVersion`, the role/user ARN for the `iam:Attach*`/`iam:Put*`
   family, the passed-role ARN for the `PassRole`+compute family). Resolution is
   index membership against the scope generation's scanned IAM `:CloudResource`
   nodes (the same in-memory ARN join index #805 / #1135 use), with this ladder:
   - **exact ARN match** in the join index → resolve to that node uid.
   - **single-prefix/glob** of the resource pattern (`arn:…:policy/team-*`)
     matching **exactly one** scanned node → resolve.
   - **wildcard target** (`*`, or a pattern matching **many** scanned nodes, or
     `is_wildcard_resource = true`) → **NOT** an edge. Recorded
     `skipped_ambiguous`. This is the deliberate conservative choice: a dangerous
     action on `Resource: "*"` is real and worth surfacing, but it does not name
     a single target node, so we refuse to fabricate a `CAN_ESCALATE_TO` edge to
     an arbitrary node. (The general `CAN_PERFORM` follow-up is where wildcard
     scope is modeled honestly.)
   - **zero matches** (target ARN never scanned, including a cross-account ARN
     whose account was not in this scope) → skip, recorded `skipped_unresolved`.

6. **Cross-account.** Identical to #805's trust-boundary rule: an edge to a
   cross-account target resolves **only** if that account+region IAM resource was
   scanned in the same scope generation (it would be in the join index). We never
   fabricate a node from an ARN string alone.

7. **No fabrication, ever.** A primitive that arms but whose target does not
   resolve to exactly one scanned node produces **no edge and no node** — it is
   counted, not dropped silently, and not dangled.

The honest accounting surface is a per-reason skip tally (a counter dimension,
§6), so an operator can see *why* escalation edges are missing
(`skipped_ambiguous` dominating means wildcard policies, not a reducer bug).

---

## 3. The catalog

Each row: the primitive token (the edge's `primitive` property value), the
required lowercase action set (ALL must be present), the target identity (which
`resources[]` ARN is resolved), and the rationale/citation. Citations are to the
two canonical, widely cited public catalogs of IAM privilege-escalation methods:

- **Rhino Security Labs**, "AWS IAM Privilege Escalation – Methods and
  Mitigation" (Spencer Gietzen) — the original 21-method catalog.
- **Bishop Fox**, "Privilege Escalation in AWS" and the `iam-vulnerable` lab —
  corroborating action requirements.

These are descriptive references for *why each action combination escalates*;
they are not load-bearing for code behavior. The code's behavior is fully
specified by §2 + this table.

### 3.1 Single-action policy-mutation primitives

| Primitive | Required actions (lowercase, ALL) | Target identity (resolved from `resources[]`) | Rationale |
| --- | --- | --- | --- |
| `iam_create_policy_version` | `iam:createpolicyversion` | the **policy** ARN | Set a new default policy version with admin permissions; escalates to whatever the policy is attached to. (Rhino #1) |
| `iam_set_default_policy_version` | `iam:setdefaultpolicyversion` | the **policy** ARN | Roll the policy back/forward to a more-permissive existing version. (Rhino #2) |
| `iam_attach_user_policy` | `iam:attachuserpolicy` | the **user** ARN | Attach a managed (e.g. `AdministratorAccess`) policy to a user. (Rhino #5) |
| `iam_attach_role_policy` | `iam:attachrolepolicy` | the **role** ARN | Attach a managed admin policy to a role. (Rhino #6) |
| `iam_attach_group_policy` | `iam:attachgrouppolicy` | the **group** ARN | Attach a managed admin policy to a group (escalates the group's members). (Rhino #7) |
| `iam_put_user_policy` | `iam:putuserpolicy` | the **user** ARN | Write an inline admin policy onto a user. (Rhino #8) |
| `iam_put_role_policy` | `iam:putrolepolicy` | the **role** ARN | Write an inline admin policy onto a role. (Rhino #9) |
| `iam_put_group_policy` | `iam:putgrouppolicy` | the **group** ARN | Write an inline admin policy onto a group. (Rhino #10) |
| `iam_update_assume_role_policy` | `iam:updateassumerolepolicy` | the **role** ARN | Rewrite a role's trust policy to make it assumable by an attacker-controlled principal. (Rhino #17) |

### 3.2 Credential / login primitives (escalate to the target user)

| Primitive | Required actions (lowercase, ALL) | Target identity | Rationale |
| --- | --- | --- | --- |
| `iam_create_access_key` | `iam:createaccesskey` | the **user** ARN | Mint a long-lived access key for another user and act as them. (Rhino #11) |
| `iam_create_login_profile` | `iam:createloginprofile` | the **user** ARN | Set a console password on a user that has none. (Rhino #12) |
| `iam_update_login_profile` | `iam:updateloginprofile` | the **user** ARN | Reset another user's console password. (Rhino #13) |

### 3.3 Group-membership primitive

| Primitive | Required actions (lowercase, ALL) | Target identity | Rationale |
| --- | --- | --- | --- |
| `iam_add_user_to_group` | `iam:addusertogroup` | the **group** ARN | Add yourself (or a controlled user) to a privileged group. (Rhino #16) |

### 3.4 `PassRole` + compute-create primitives (escalate to the passed role)

These are **multi-action**: `iam:passrole` is inert alone. Each requires
`iam:passrole` **AND** a specific compute-create (and, where the compute is not
itself an execution surface, an invoke/start action). The target is the **passed
role** — the resource the compute service will assume. Per §2 rule 4 the
primitive arms only when **every** listed action is present.

| Primitive | Required actions (lowercase, ALL) | Target identity | Rationale |
| --- | --- | --- | --- |
| `passrole_lambda` | `iam:passrole`, `lambda:createfunction`, `lambda:invokefunction` | the **passed role** ARN | Create a Lambda with a high-priv execution role and invoke it. (Rhino #3) |
| `passrole_ec2` | `iam:passrole`, `ec2:runinstances` | the **passed role** ARN | Launch an EC2 instance with an instance profile and read its credentials. (Rhino #4) |
| `passrole_glue_dev_endpoint` | `iam:passrole`, `glue:createdevendpoint` | the **passed role** ARN | Create a Glue dev endpoint running as the passed role. (Rhino #15) |
| `passrole_cloudformation` | `iam:passrole`, `cloudformation:createstack` | the **passed role** ARN | Deploy a CloudFormation stack that acts as the passed role. (Rhino #14) |
| `passrole_sagemaker_notebook` | `iam:passrole`, `sagemaker:createnotebookinstance` | the **passed role** ARN | Create a SageMaker notebook with the passed role and exfiltrate its credentials. (Bishop Fox) |
| `passrole_datapipeline` | `iam:passrole`, `datapipeline:createpipeline`, `datapipeline:putpipelinedefinition`, `datapipeline:activatepipeline` | the **passed role** ARN | Stand up a Data Pipeline that runs as the passed role. (Rhino #18) |

For the `PassRole` family the **passed-role** identity is taken from the
`iam:passrole` statement's `resources[]` (the role the principal is allowed to
pass). The compute-create actions usually carry their own (non-IAM) resource
patterns; those are **not** the escalation target and are ignored for target
resolution. If the `iam:passrole` statement's resource is a wildcard or resolves
to many/zero scanned roles, the primitive is `skipped_ambiguous` /
`skipped_unresolved` exactly like the single-action families.

### 3.5 The deferred-by-design primitive

| Primitive | Required actions | Behavior |
| --- | --- | --- |
| `sts:assumerole` | `sts:assumerole` | **Emits nothing.** Role assumption is already modeled by the `CAN_ASSUME` trust edge (#1134 PR2 / PR #1214). Duplicating it as `CAN_ESCALATE_TO` would create two relationships for one real-world capability and double-count blast radius. The extractor explicitly recognizes `sts:assumerole` and records it under a dedicated `deferred_can_assume` tally so the deferral is observable, not silent. |

---

## 4. Extraction algorithm (per scope generation)

1. Build the in-memory `cloudResourceJoinIndex` from the scope generation's
   `aws_resource` facts (bounded, O(1) per lookup, no per-edge graph round trip —
   mirrors #805 §5.1).
2. Group the scope's `aws_iam_permission` facts by `principal_arn`.
3. Resolve the principal's own node uid via the join index. If the principal was
   not scanned, skip the whole principal (`skipped_unresolved`) — there is no
   source node to anchor an edge on.
4. For the principal, build the **trusted-Allow action set** = the union of
   `actions[]` from its statements that are `Allow`, unconditioned
   (`has_conditions = false`), and free of `not_actions`/`not_resources`. Track
   each contributing statement so target resolution can pull the right
   `resources[]`.
5. Build the principal's **deny-touched action set** = union of `actions[]` from
   its `Deny` statements (so a Deny on `iam:*` removes every `iam:` primitive).
6. For each catalog primitive: it **arms** iff every required action is present in
   the trusted-Allow set (per §2 rule 4 wildcard rules) **and** none of its
   required actions is in the deny-touched set. Tally the skip reason otherwise.
7. For an armed primitive, resolve the **target** from the relevant statement's
   `resources[]` per the §2 resolution ladder. Exactly-one scanned-node match →
   edge row; wildcard/many → `skipped_ambiguous`; zero → `skipped_unresolved`.
8. Deduplicate edge rows by `(principal_uid, target_uid)`; when more than one
   primitive resolves to the same `(principal_uid, target_uid)`, **merge their
   primitive tokens into one sorted, deduplicated `primitives[]` list** carried as
   an edge property (see §5 keying). Sort all rows for byte-stable batched writes.

A principal escalating **to itself** (`principal_uid == target_uid`, e.g.
`iam:CreateAccessKey` on `Resource: <self>`) is dropped without counting it as a
skip — a self-loop carries no escalation truth (same rule as #805's self-loop
guard).

---

## 5. Edge keying — the one keying decision, resolved (no fork)

The perf doc (`cypher-performance.md`) and `cypher-query-rigor` forbid a
property-keyed `MERGE` (it misses NornicDB's relationship hot path and risks the
20s timeout, #805 §5.3). So the `primitive` **cannot** be part of the MERGE
identity, and the relationship **type** must be the static token
`CAN_ESCALATE_TO` (not `CAN_ESCALATE_TO_<primitive>`), exactly like
`RUNS_IMAGE` (#388) and `ALLOWS_INGRESS`/`TO` (#1135).

That leaves the genuinely-flagged question: *what happens when two primitives
reach the same `(principal, target)`?* A scalar `primitive` property would lose
one. **Resolution (chosen, not a fork):** the MERGE keys on the stable identity
`(principal_uid, CAN_ESCALATE_TO, target_uid)`; the **set** of primitives that
reached that target is computed deterministically **in memory** (sorted, deduped)
and written as a single `SET rel.primitives = row.primitives` list property. This
is the same static-MERGE-then-SET-mutable-properties shape both reference writers
use; the list is overwritten wholesale each generation, so the write stays
**idempotent** (same input → same sorted list → same edge). No node-side modeling
(Option D) is needed because the primitive is not part of the *identity* — it is a
descriptive attribute of an edge whose identity is fully captured by its two
endpoints. This keeps the MERGE on a static token over two uid-indexed
`MATCH (:CloudResource {uid})` anchors.

```cypher
UNWIND $rows AS row
MATCH (p:CloudResource {uid: row.principal_uid})
MATCH (t:CloudResource {uid: row.target_uid})
MERGE (p)-[rel:CAN_ESCALATE_TO]->(t)
SET rel.primitives = row.primitives,
    rel.primitive_count = row.primitive_count,
    rel.scope_id = row.scope_id,
    rel.generation_id = row.generation_id,
    rel.evidence_source = row.evidence_source
```

Both endpoints are existing IAM `:CloudResource` nodes keyed on the existing
`cloud_resource_uid` keyspace. **Edge-only**: no new node type, no new keyspace,
no schema constraint. A row whose principal or target node is absent produces no
edge (two `MATCH`es precede the `MERGE`), so a stale/partial generation never
fabricates a node.

Retract before reproject, scoped to this reducer's `evidence_source`
(`reducer/iam-escalation`) and `scope_id`, never touching endpoint nodes — same
as #388/#1135:

```cypher
MATCH (p:CloudResource)-[rel:CAN_ESCALATE_TO]->()
WHERE rel.scope_id IN $scope_ids
  AND rel.evidence_source = $evidence_source
DELETE rel
```

---

## 6. Readiness, concurrency, telemetry

- **Readiness gate.** Gate the edge domain on
  `GraphProjectionKeyspaceCloudResourceUID` /
  `GraphProjectionPhaseCanonicalNodesCommitted` for the intent's scope generation
  (the IAM principal/role/user/group/policy nodes are AWS resource nodes
  materialized by #805's `aws_resource` slice). A not-ready miss is a **retryable**
  error so the durable queue re-runs once the nodes commit — identical to #805 /
  #388 / #1135.
- **Concurrency.** Conflict domain = the reducer-owned `CAN_ESCALATE_TO` edges
  for one `scope_id`. Retract+rewrite is per-scope and idempotent; different
  scopes are independent; same-scope retries converge on the same edge set. No
  worker-count reduction, no batch-size-1, no serialization — the MERGE identity
  is stable and the write is idempotent under concurrent execution (per
  "Serialization Is Not A Fix").
- **Telemetry.** New counters (`eshu_dp_` prefix), each recorded even at zero so
  the time series exists:
  - `eshu_dp_iam_escalation_edges_total` — edges committed (no label; one edge
    family).
  - `eshu_dp_iam_escalation_skipped_total{skip_reason}` — bounded reasons:
    `skipped_ambiguous`, `skipped_unresolved`, `skipped_deny`,
    `skipped_conditioned`, `skipped_not_action_resource`, `skipped_incomplete`,
    `deferred_can_assume`.
  - Span `reducer.iam_escalation_materialization` and a completion log with
    fact/principal/edge counts, the skip tally, and stage durations.
  High-cardinality values (principal ARNs, primitive tokens) stay in spans/logs,
  never in metric labels.

---

## 7. Performance and observability evidence markers

This is a new hot-path graph-write slice, so it carries the evidence markers the
CI gate requires. The write path is the same shape class as the proven
`RUNS_IMAGE` (#388) and reachability (#1135) writers — batched `UNWIND`
`MATCH`/`MATCH`/`MERGE`, static relationship-type token, no per-edge round trip,
no N+1.

Performance Evidence: focused writer benchmark
`BenchmarkIAMEscalationEdgeWriter` measures statement construction + batching for
a realistic per-scope edge count against a no-op group executor, isolating
Eshu-owned write-path work; it stays in the same shape class as the shipped
COVERS / RUNS_IMAGE / reachability writers (single batched MERGE per chunk, no
N+1). No production-cardinality NornicDB regression: the edge family adds one
static-token MERGE shape over two uid-indexed anchors, identical to the
already-measured #388/#1135 writers.

Observability Evidence: new `eshu_dp_iam_escalation_edges_total` and
`eshu_dp_iam_escalation_skipped_total{skip_reason}` counters plus the
`reducer.iam_escalation_materialization` span and completion log expose edges
committed, skip reason breakdown, and stage timing for an operator at 3 AM.

---

## 8. Why this is build-safe

- **Accuracy:** only `Allow`, unconditioned, non-`NotAction` statements with a
  complete primitive and an unambiguous single scanned target become an edge.
  Wildcard/many/zero/Deny/conditioned all degrade to a counted skip, never an
  edge. No fabrication, no cross-account invention.
- **Performance:** mirrors a measured, shipped write shape; bounded in-memory
  resolution; static-token MERGE; benchmark + markers included.
- **Concurrency:** idempotent MERGE on a stable identity, evidence-scoped
  retract, readiness-gated; no serialization.

The remaining judgment call that genuinely needs a **human security reviewer** is
the catalog content in §3 (is the action→target mapping correct and complete
enough?) and the conservative confidence model in §2 (are the skip rules the
right trade-off?). Those are this PR's review focus.
