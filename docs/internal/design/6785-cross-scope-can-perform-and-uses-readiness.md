# Cross-Scope CAN_PERFORM And USES Readiness (#6785)

Status: two owner-decided fixes on `gate/6785-golden-corpus-zero-floors`.

1. CAN_PERFORM resolves identity-policy targets across scopes in the same AWS
   account.
2. `workload_cloud_relationship_materialization` defers until the
   WorkloadInstance nodes its anchors name exist. It is no longer on the
   blanket cross-scope reopen list.

## 1. Problem

- **CAN_PERFORM never resolves in production.** The handler loads facts for
  one `(scope, generation)` and joins principal and target ARNs only inside
  that scope. The awscloud collector emits `aws_iam_role` and
  `aws_iam_permission` into `aws:<acct>:<claim-region>:iam`
  (`services/iam/scanner.go`). S3, KMS, and the other catalog targets land in
  their own service scopes (`services/s3/scanner.go`, `aws:<acct>:<region>:s3`).
  So every production evaluation ends as `skipped_unresolved`. The B-7 cassette
  hid this by putting all three facts in one lambda scope (review finding F1).
- **USES races WorkloadInstance materialization.** The workload-cloud handler
  gates only on CloudResource nodes in its own scope. Its writer is MATCH-only
  on `(Workload)<-[:INSTANCE_OF]-(WorkloadInstance)`, so if the instance is
  missing the handler succeeds with nothing written. The earlier fix added the
  domain to `crossScopeCorrelationReopenDomains`. That replays it on every
  shard drain, for every AWS scope (F3), and its comment misdescribed the
  retract (F2).

## 2. Pre-Edit Gate

**Entry point.**
- `cmd/reducer/main.go` builds `reducer.DefaultHandlers`.
- `internal/reducer/defaults_additive_domains_cloud_{relationships,posture}.go`
  wire both handlers.
- The durable reducer queue (`storage/postgres/reducer_queue*.go`) claims
  intents and fails or acks them.

**Phase ordering.**
- The projector fans a scope generation out to intents:
  - `projector/workload/cloud/reducer_intent.go` enqueues workload-cloud for
    any scope with `aws_resource` facts.
  - `projector/cloud/aws/iam/perform` enqueues CAN_PERFORM for scopes with
    identity or resource-policy permissions.
- Both intents are claim-gated on `aws_resource_materialization:<scope>`
  `canonical_nodes_committed` for their own scope
  (`reducerClaimReadinessRequirementsSQL`).
- Target scopes run the same projector and node pipeline independently. There
  is no cross-scope ordering.
- WorkloadInstance nodes come from `workload_materialization` in the repo
  scope. That can itself wait on the relationship maintenance pass.

**Data dependencies.**
- CAN_PERFORM needs:
  - the iam scope's role and permission facts;
  - the target's `aws_resource` fact in its scope's active generation;
  - the target's committed CloudResource node, keyed by the same
    `cloudjoin` uid.
- USES needs the CloudResource node, which is already gated, and the
  WorkloadInstance node (new gate).

**Re-trigger.** Both handlers already return retryable, self-classified
readiness errors. The queue re-offers the row after `RetryDelay`, 30 s by
default. Section 5 covers what happens when a later generation lands.

## 3. CAN_PERFORM Cross-Scope Resolution

### 3.1 What is resolved cross-scope

- **What is requested.** From the loaded identity-policy statements (inline or
  attached managed, `Allow`), collect the exact `Resource` ARNs that
  `iamCanPerformResourceTypeOfARN` classifies as a catalog target type.
- **Account.** Only ARNs whose account segment is empty (S3) or equal to the
  intent scope's account. Other accounts are never looked up.
- **What is not resolved cross-scope:**
  - **Glob patterns** stay local-only. A cross-scope lookup that returned only
    the exactly-named ARNs would give the glob matcher a partial view. It could
    then report `single_glob` against one bucket when the account has two:
    a false edge. Cross-scope globs are counted as their own telemetry outcome
    and left as a follow-up.
  - **Resource-policy grantee resolution.** A bucket policy in the s3 scope
    naming a role in the iam scope stays same-scope. If both scopes wrote the
    same edge, they would overwrite each other's `rel.scope_id` and
    `grant_sources`. That needs an owner-level ownership design. It is out of
    scope here.

### 3.2 Bounded lookup (`CrossScopeTargetLoader`)

A new port lives in `iamcan`. Its Postgres implementation is
`IAMCanPerformCrossScopeTargetStore`. Each request carries the account, the
intent scope (excluded from the search), and `(service_kind, region, arn)`
triples. For S3, region is empty, meaning any region.

- **Query 1: scope state, one round trip.** Select `ingestion_scopes` rows
  matching `aws:<acct>:<region|any>:<service>`. For each row, return:
  - `active_generation_id`;
  - whether that generation's status is `active`;
  - whether `graph_projection_phase_state` has `canonical_nodes_committed` on
    `cloud_resource_uid` for `aws_resource_materialization:<scope>` at that
    generation;
  - whether any generation is still `pending`.
- **Query 2: facts, per scope.** For each candidate scope with an active
  generation, call the existing
  `FactStore.ListFactsByKindAndPayloadValue(scope, activeGen, "aws_resource", "arn", arnsForThatService)`.
  It uses `fact_records_scope_generation_idx` and transfers only matching rows.
- **Ordering.** Readiness is sampled before the fact load, and the load is
  pinned to the generation id that sample saw. That rules out the
  #5875-class TOCTOU, where a generation activating between load and check
  would be judged against a newer committed flag.
- **Cost.**
  - Scope queries: 1.
  - Fact queries: the number of distinct `(service, region)` pairs named. S3
    fans out to the account's S3 regions.
  - Rows returned: at most the number of requested ARNs per scope.
  - It never scans the whole graph or other accounts.
- **Measured before implementing.** On throwaway Postgres 16 with 105 000
  scopes (85 000 AWS, spread over 50 accounts x 17 regions x 100 services, plus
  20 000 git) and 525 000 generations, the scope-state query took 10.9, 11.3,
  and 13.1 ms over three runs. `ingestion_scopes` is a parallel seq scan
  (3 530 buffer hits), because the default collation cannot use the PK for
  `LIKE`. The phase and pending probes are index-only scans. It returned
  exactly 18 rows: 17 S3 regions plus 1 KMS region.

### 3.3 Per-ARN decision

| Found in scope S? | S nodes committed? | Other candidate scope never activated but pending? | Outcome |
| --- | --- | --- | --- |
| yes | yes | n/a | `resolved`: the exact-ARN target is usable |
| yes | no | n/a | `not_ready` (defer) |
| no | n/a | yes | `not_ready` (defer) |
| no | n/a | no | `unresolved`: the honest `skipped_unresolved` |

- **A newer pending generation is ignored.** It does not defer when an active
  one exists. Policies often name resources that don't exist, so deferring on
  every rolling collection would hold CAN_PERFORM at the bound on each cycle.
- **A scope that has never activated and is not pending** (failed, or absent)
  is settled: nothing is coming.
- **Foreign targets never enter glob matching.** Resolved foreign facts build
  a separate `cloudjoin` index used only for exact-ARN matching.
- **Quarantine.** Foreign facts that fail decode are not re-reported here.
  They belong to, and are quarantined by, their own scope's handlers.

### 3.4 Bound

- Any `not_ready` returns `iamCanPerformTargetNotReadyError`, class
  `iam_can_perform_target_not_ready`.
- The class is retryable and enrolled in `nonCountingReducerRetryFailureClasses`
  and the golden-gate readiness list.
- **Why the bound is elapsed time, not attempts.** Readiness classes freeze
  `attempt_count`, and `TestEveryReadinessFailureClassIsEnrolled` forces this
  one to be a readiness class. An attempt bound could never fire (see the
  `awsCloudRuntimeDriftStatePendingMaxWait` precedent). So the bound is
  `crossscope.ProducerReadinessMaxWait`, 30 min, measured from
  `crossscope.ReadinessCycleAnchor` (CycleStartedAt, which is fresh on reopen).
  A zero anchor keeps deferring.
- **Backoff** is the queue's `RetryDelay` at the frozen attempt. **Past the
  bound**, `not_ready` targets commit as `unresolved`; `abandoned` is counted
  and logged at WARN.

## 4. USES WorkloadInstance Readiness

- **Lookup.** After extraction, collect the distinct `(workload_id,
  environment)` anchors from the rows. Ask
  `WorkloadInstanceExistenceLookup` (graph read, mirroring
  `GraphContainerImageExistenceLookup`) which of them exist. It uses the
  writer's own shape: `UNWIND $anchors AS anchor MATCH (w:Workload
  {id: anchor.workload_id})<-[:INSTANCE_OF]-(i:WorkloadInstance) WHERE
  i.environment = anchor.environment`. So "ready" means exactly "the writer's
  MATCH will bind".
- **Defer.** If any anchor is missing and the bound is not reached, return
  `workloadCloudRelationshipInstancesNotReadyError`, class
  `workload_cloud_relationship_instances_not_ready`. It is non-counting and has
  the same 30-min elapsed bound. The whole intent defers, so nothing is
  retracted and the prior generation's USES edges stay readable meanwhile.
- **Past the bound.** Write all rows (missing anchors are MATCH no-ops). Count
  `CanonicalWrites` only for ready anchors, and log the missing-anchor count
  plus a bounded sample of 10 at WARN.
- **Nil lookup** (test wiring) keeps the old behavior.
- **Reopen list.** The domain comes off `crossScopeCorrelationReopenDomains`
  (list, comment, and pin test restored to `origin/main`), so F3's per-drain
  replay cost is gone rather than measured.

## 5. Re-evaluation, Supersession, Idempotency

```mermaid
sequenceDiagram
    participant IAMProj as projector (iam scope)
    participant S3Proj as projector (s3 scope)
    participant Q as reducer queue
    participant H as CAN_PERFORM handler
    participant PG as Postgres
    participant G as graph
    IAMProj->>Q: enqueue iam_can_perform (iam gen N)
    Q->>H: claim (own-scope nodes committed)
    H->>PG: scope states for aws:acct:*:s3
    PG-->>H: s3 scope pending, never active
    H-->>Q: iam_can_perform_target_not_ready (retry, non-counting)
    S3Proj->>PG: activate s3 gen; aws_resource_materialization commits nodes
    Q->>H: re-claim after RetryDelay
    H->>PG: scope states (active + committed), then facts by arn at pinned gen
    H->>G: retract scope edges (if prior gen) then MERGE role-CAN_PERFORM->bucket
    H-->>Q: succeeded
```

- **A later target generation.** Nothing reschedules a succeeded CAN_PERFORM
  row. A resource added in s3 gen N+1 is picked up by the next iam generation,
  so staleness is at most one IAM collection interval.
- **Rejected alternative: the completion fanout** (producer
  `aws_resource_materialization`, consumer `iam_can_perform`):
  - It needs a new emission CTE on the hottest ack path.
  - It is unscoped, so every AWS ack in any account would reschedule every
    account's IAM row, roughly every 2 s during a collection window.
  - The owner can override this choice.
- **Removals.** An edge to a retracted target node goes with the node or at
  the next iam generation's retract. USES has the same one-interval bound for
  an instance that appears after its defer bound expired.
- **Idempotency.**
  - Both writers MERGE on the endpoint pair.
  - Retract is scope-wide by `rel.scope_id` and evidence source. It is skipped
    only on attempt 1 of a scope's first generation (`PriorGenerationCheck`).
    Otherwise every run retracts and rewrites (F2: the corrected comment, plus
    a `PriorGenerationCheck=true` replay test).
  - Defer paths return before any retract or write, so a deferred attempt
    changes nothing.
- **Locks and leases.**
  - The claim fence stays `(scope, domain)`.
  - The new reads are plain SELECT and read-only Cypher with no row locks, so
    they add no lock-ordering edge.
  - The only new graph contention: a CAN_PERFORM MERGE now touches a bucket
    node that the s3 scope's node writer may be SETting concurrently. Both are
    single-statement and idempotent. A write conflict surfaces as the existing
    retryable graph-write class, and nothing is serialized.

## 6. Telemetry

- `eshu_dp_reducer_readiness_waits_total{domain, outcome=deferred|abandoned}`,
  one per deferred or bound-expired evaluation, for both domains.
- `eshu_dp_iam_can_perform_cross_scope_targets_total{outcome}`, one per
  requested target (`resolved`, `unresolved`, `not_ready`, `abandoned`,
  `glob_local_only`). Defer and abandonment logs carry counts and a bounded
  sample. Labels are closed sets; no ARN, scope, or workload value is a label.

## 7. Cassette

- The role (prod and stage anchors) and the inline permission move to
  `aws:123456789012:us-east-1:iam` (the claim-region convention of the
  cassette's existing iam scope). The bucket moves to
  `aws:123456789012:us-east-1:s3`. Stable keys and values stay synthetic.
- Expected B-7: CAN_PERFORM 1 via the cross-scope path, and USES 2 from the iam
  scope once the instances exist.
