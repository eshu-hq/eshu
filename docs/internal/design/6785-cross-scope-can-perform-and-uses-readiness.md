# Cross-Scope CAN_PERFORM And USES Readiness (#6785)

Status: two owner-decided fixes on `gate/6785-golden-corpus-zero-floors`.

1. CAN_PERFORM resolves identity-policy targets across scopes in the same AWS
   account.
2. `workload_cloud_relationship_materialization` waits for the
   WorkloadInstance nodes its anchors name. It is no longer on the blanket
   cross-scope reopen list.

Both handlers commit first and then wait (section 3.4). The wait is bounded
by a `(scope_id, domain)` ledger row that survives supersession of the
per-generation queue row (review finding R2-F1).

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
- USES needs the gated CloudResource node and the WorkloadInstance (new gate).

**Re-trigger.** Retryable readiness errors; the queue re-offers the row after
`RetryDelay` (30 s default). Section 5 covers later generations.

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
- **Measured before implementing** (Postgres 16, 105 000 scopes, 525 000
  generations): 10.9-13.1 ms over three runs; a seq scan of `ingestion_scopes`
  (no `LIKE` index under the default collation) plus index-only probes.

### 3.3 Per-ARN decision

The expected scope comes from the ARN: `aws:<account>:<region>:<service>`,
or any region's `aws:<account>:*:s3` for a bucket ARN, which names no region.

| Found in committed S? | Found, uncommitted? | Expected scope registered? | Never-active pending candidate? | Outcome |
| --- | --- | --- | --- | --- |
| yes | n/a | yes | n/a | `resolved` |
| no | yes | yes | n/a | `not_ready` (defer) |
| no | no | yes | yes | `not_ready` (defer) |
| no | no | no | n/a | `scope_unregistered` (defer): the iam scope ran first |
| no | no | yes | no | `unresolved`: the honest `skipped_unresolved` |

- **A newer pending generation beside an active one does not defer.** Policies
  name resources that don't exist; deferring on rolling collection would hold
  every cycle at the bound. A failed, never-active scope is settled.
- **Wildcard and glob patterns name no concrete scope**, so they keep today's
  semantics: local-only matching, and `skipped_ambiguous`/`skipped_unresolved`.
- **Known limits.** An S3 bucket is satisfied by any registered s3 scope of the
  account, so a bucket in a second region whose s3 scope is not registered yet
  reads settled while another region's is. A service or region this deployment
  never collects keeps its targets missing. The ready edges still commit at
  every generation's first claim; the missing set settles once, at
  first-defer + bound (`abandoned`), and later generations with the same set
  commit at once (`settled_missing`).
- **Foreign targets never enter glob matching.** Resolved foreign facts build
  a separate `cloudjoin` index used only for exact-ARN matching.
- **Quarantine.** Foreign decode failures belong to their own scope's handlers.

### 3.4 Commit first, then wait

Review finding R2-F1 showed the first version of this gate could starve. It
deferred the whole intent while any target was missing, and bounded the defer
by the queue row's own `created_at`. Reducer rows are per generation, and the
claim supersedes an older generation's `retrying` row once a newer generation
activates. The new row started a new bound. With the default AWS cadence
(scheduled plans bucket on `ESHU_WORKFLOW_COORDINATOR_RECONCILE_INTERVAL`, 30 s
by default, `go/internal/coordinator/config.go`), a persistent missing target
meant the domain never committed. Every deferral also held back the retraction
of revoked grants.

The handler now works in this order:

1. **Read the ledger row** for `(scope_id, iam_can_perform_materialization)`
   (`reducer_readiness_waits`, migration 109, `storage/postgres/readiness/wait`).
2. **Cheap poll.** If the row says this generation and queue cycle already
   committed at the row's own missing set (`crossscope.PollEligible`), ask the
   loader only about the missing ARNs. There is no fact load and no extraction.
   If the set is unchanged, write nothing and either keep waiting or settle.
   If a target resolved, fall through to the full evaluation.
3. **Full evaluation.** Load facts, classify targets (section 3.3), and call
   `crossscope.DecideWait`:

   | Missing set `M` | Ledger row | Commit? | Then |
   | --- | --- | --- | --- |
   | empty | any | yes | clear the row, succeed |
   | non-empty | settled, same fingerprint | yes | succeed, `settled_missing` |
   | non-empty | settled, new fingerprint | yes | new anchor, defer |
   | non-empty | none, or not committed at this generation, cycle, and fingerprint | yes | keep the anchor, defer or settle |
   | non-empty | committed at this generation, cycle, and fingerprint | no | defer, or settle at the bound |

4. **Commit** is the existing scope-wide retract plus rewrite. Missing targets
   are left out, so they read as unresolved. The reviewer's condition for a
   partial CAN_PERFORM commit (scope-wide retract and rewrite) holds.
5. **Write the ledger after the graph commit.** A crash between the two
   re-commits once, idempotently. It never skips a commit.
6. **Return** `iamCanPerformTargetNotReadyError` (non-counting) when the wait
   continues, or succeed.

**The bound** is `ReadinessMaxWait` (a handler field, default
`crossscope.ProducerReadinessMaxWait`, 30 min) since the ledger's
`first_deferred_at`. The ledger keeps the earliest anchor across generations
(`LEAST` in the upsert). It resets the anchor only when a settled wait sees a
different missing set. So `abandoned` fires once per (scope, domain, missing
set), at first-defer + bound, whatever the generation cadence.

**Why the commit marker includes the queue cycle.** `CycleStartedAt` is
`COALESCE(reopened_at, created_at)`. A graph rebuild re-drives reducer work
with new rows, and a maintenance pass reopens rows. In both cases the prior
commit's edges may be gone, so a new cycle always re-commits instead of
polling.

**Re-commits inside one generation.** The non-counting class freezes
`AttemptCount`, so on a scope's first generation `shouldSkipRetract` alone
would keep skipping the retract on every re-commit. That is not safe for
CAN_PERFORM: the iam fact set is fixed, but the cross-scope side is not. A
target s3 scope can activate a newer generation without a bucket an earlier
evaluation resolved, so the edge set can shrink (review P3-2). A re-commit of a
generation the ledger already committed (`crossscope.CommittedInGeneration`)
therefore always retracts, in both handlers; only the generation's first
commit may skip it
(`TestIAMCanPerformFirstGenerationReCommitRetractsShrunkTargets`,
`TestWorkloadCloudRelationshipFirstGenerationReCommitRetracts`).

**Nil ledger** (test wiring only) treats every evaluation as the first of its
queue cycle, anchored at the claim's cycle start. Production wires
`wait.Store` (`storage/postgres/readiness/wait`) through `DefaultHandlers.ReadinessWaits`
(`TestDefaultHandlersWireReadinessWaitLedger`).

## 4. USES WorkloadInstance Readiness

- **Lookup.** After extraction, collect the distinct `(workload_id,
  environment)` anchors from the rows. `workloadinstance.Check` asks
  `WorkloadInstanceExistenceLookup` (graph read, mirroring
  `GraphContainerImageExistenceLookup`) which of them exist. It uses the
  writer's own shape: `UNWIND $anchors AS anchor MATCH (w:Workload
  {id: anchor.workload_id})<-[:INSTANCE_OF]-(i:WorkloadInstance) WHERE
  i.environment = anchor.environment`. So "ready" means exactly "the writer's
  MATCH will bind".
- **Commit first, then wait.** `workloadinstance.Wait` applies the same
  `crossscope.DecideWait` as section 3.4, keyed
  `(scope_id, workload_cloud_relationship_materialization)`. The commit writes
  every row; a row whose instance is missing is a MATCH no-op. The handler then
  returns `workloadinstance.NotReadyError`, class
  `workload_cloud_relationship_instances_not_ready` (non-counting). Ledger keys
  are `workload_id` and `environment` joined by the ASCII unit separator
  (`AnchorKey`).
- **Cheap poll.** An unchanged poll looks up only the missing anchors, with no
  fact load and no graph write. When an instance appears, the next evaluation
  re-commits once and the MATCH binds.
- **`CanonicalWrites`** counts only rows whose anchor exists, and only on an
  evaluation that committed.
- **Nil lookup** (test wiring) keeps the old behavior: commit, no wait.
- **Reopen list.** The domain comes off `crossScopeCorrelationReopenDomains`
  (list, comment, and pin test restored to `origin/main`), so F3's per-drain
  replay cost is gone rather than measured.

## 5. Re-evaluation, Supersession, Idempotency

```mermaid
sequenceDiagram
    participant Q as reducer queue
    participant H as CAN_PERFORM handler
    participant L as reducer_readiness_waits
    participant PG as Postgres (facts, scopes)
    participant G as graph
    Q->>H: claim gen N (own-scope nodes committed)
    H->>L: get (scope, domain): no row
    H->>PG: facts + cross-scope targets: bucket B not ready
    H->>G: retract scope edges, MERGE ready edges
    H->>L: upsert anchor=t0, missing={B}, committed=(N, cycle, fp)
    H-->>Q: iam_can_perform_target_not_ready (non-counting)
    Q->>H: re-claim after RetryDelay
    H->>L: get: committed at N, same cycle
    H->>PG: targets for {B} only: still not ready
    H-->>Q: not ready (no graph write, no ledger write)
    Note over Q: gen N+1 activates; claim supersedes N
    Q->>H: claim gen N+1
    H->>L: get: anchor t0 kept
    H->>PG: facts + targets: B still missing
    H->>G: retract + rewrite (revoked grants gone now)
    H->>L: upsert committed=(N+1, cycle, fp), anchor t0
    H-->>Q: not ready
    Note over H: at t0 + MaxWait
    Q->>H: re-claim gen N+1
    H->>L: settle (settled_at), abandoned counted once
    H-->>Q: succeeded
```

- **Supersession.** The superseded row is terminal, and the ledger row is not
  touched by it. The next generation commits at its first claim and keeps the
  anchor. Proven on the real queue by
  `TestReadinessWaitSurvivesSupersessionLive` (removing the ledger makes its
  last step keep deferring).
- **Consumers of the success ack.** Commit-first delays a row's success ack
  until the missing set empties or settles, although its edges are already
  written. No consumer reads "no success ack" as "no edges written":
  - neither domain is a producer in `crossscope.dependencyCatalog`;
  - `cross_scope_completion_events` only accepts `ci_cd_run_correlation` and
    `container_image_identity` (migration 093);
  - the writers' edge phase names (`iam_can_perform_edge`,
    `workload_cloud_relationship_edge`) are statement metadata with no
    Postgres reader;
  - the remaining readers (`status_queries.go`,
    `generation_lifecycle_sql.go`, `queue_observer.go`,
    `localsupervisor` content-index `open_work`, golden-gate drains) count
    outstanding rows only. A waiting row keeps them outstanding, as the
    whole-intent defer already did.
- **A later target generation is being built in a separate PR under #6785**
  (owner decision, 2026-09-19). A bucket
  added in s3 gen N+1 after CAN_PERFORM succeeded waits for the next iam
  generation. The completion fanout cannot re-enqueue just that account's row:
  events are keyed by producer domain only, and the fanout reschedules every
  consumer row. Scoping it needs an account key on
  `cross_scope_completion_events` (migration) plus an emission CTE on the
  `aws_resource_materialization` ack, the hottest AWS ack path. When it lands,
  CAN_PERFORM keeps commit-first and drops the poll; the ledger stays for USES
  and for `abandoned`.
- **Removals.** A revoked grant is retracted at the first claim of the next
  iam generation, even while another target is missing
  (`TestIAMCanPerformRevokedGrantRetractsAtFirstEvaluation`).
- **Idempotency.**
  - Both writers MERGE on the endpoint pair.
  - Retract is scope-wide by `rel.scope_id` and evidence source. It is skipped
    only for the first commit of a scope's first generation: attempt 1,
    `PriorGenerationCheck` false, and no earlier commit of this generation in
    the ledger (`crossscope.CommittedInGeneration`). A re-commit inside the
    first generation retracts, because the resolved set can shrink between
    evaluations: a target s3 scope can activate a newer generation without a
    bucket that an earlier evaluation resolved (review P3-2,
    `TestIAMCanPerformFirstGenerationReCommitRetractsShrunkTargets`).
  - A poll re-checks only the stored missing keys, so a resolved target that
    disappears mid-wait is not noticed by a poll. It is retracted at the next
    commit: when a missing key resolves, or at the next iam generation.
  - An unchanged poll performs no graph write and no ledger write.
  - A replayed ledger upsert leaves the row unchanged; racing upserts at the
    same epoch keep the earlier anchor
    (`TestReadinessWaitConcurrentUpsertsKeepEarliestAnchorLive`).
- **Locks and leases.**
  - The claim fence stays `(scope, domain)`, so at most one live worker writes
    a ledger key. A lease-expired straggler is the only concurrent writer.
  - `anchor_epoch` fences stragglers (review P3-1). An anchor reset (a settled
    or cleared row seeing a new missing set) and a clear each move the row to
    the next epoch. The upsert's `ON CONFLICT ... WHERE EXCLUDED.anchor_epoch >=
    wait.anchor_epoch` drops a write from an older epoch, so a straggler that
    read the row before the reset cannot restore the older anchor through
    `LEAST`. A clear keeps the row as a tombstone at the next epoch instead of
    deleting it, so a straggler cannot re-insert a stale wait either.
    `TestReadinessWaitStaleWriterCannotUndoResetLive` races the two writers
    over 20 rounds, forcing the straggler to commit last on half of them;
    `TestReadinessWaitStaleWriterCannotResurrectClearedWaitLive` covers the
    clear. Both failed before the fence (the anchor was restored; the cleared
    wait came back).
  - Inside one epoch a straggler can still win last-writer-wins on the other
    columns. At worst it rewinds the commit marker, which costs one idempotent
    re-commit, or un-settles a row whose anchor is already past the bound, which
    settles again on the next evaluation. Truth is unaffected either way: ready
    edges commit before any ledger write.
  - A fenced write logs `readiness wait write dropped by the anchor epoch
    fence` with `scope_id`, `domain`, `readiness_wait_operation`, and
    `anchor_epoch`.
  - Rows are never deleted. They are bounded by the key: two domains, so at
    most two rows per scope that ever waited, each with at most 500 missing
    keys and a tombstone with none. `ingestion_scopes` and `scope_generations`
    are not garbage-collected either. A retention sweep would have to prove no
    straggler can still write, which a lease timeout does not guarantee
    (review P3-3).
  - Ledger statements are single-row primary-key reads, upserts, and deletes,
    run outside any open transaction. They add no lock-order edge, and the
    claim query and `fact_work_items` are unchanged.
  - The new reads are plain SELECT and read-only Cypher with no row locks.
  - The only new graph contention: a CAN_PERFORM MERGE now touches a bucket
    node that the s3 scope's node writer may be SETting concurrently. Both are
    single-statement and idempotent. A write conflict surfaces as the existing
    retryable graph-write class, and nothing is serialized.

## 6. Telemetry

- `eshu_dp_reducer_readiness_waits_total{domain, outcome}`, one per evaluation
  that has a missing set, for both domains:
  - `deferred`: the not-ready error was returned (after any commit);
  - `abandoned`: the missing set settled at first-defer + bound; once per
    (scope, domain, missing set);
  - `settled_missing`: a later evaluation committed at once on an already
    settled set, with no defer and no poll.
- `eshu_dp_iam_can_perform_cross_scope_targets_total{outcome}`, one per
  requested target, emitted only by evaluations that commit, so `not_ready`
  no longer scales with polls (review P3-C). Outcomes: `resolved`,
  `unresolved`, `not_ready`, `scope_unregistered`, `abandoned` (committed as
  unresolved because the wait settled), `glob_local_only`.
- Wait logs carry `readiness_wait_outcome`, the missing count, `committed`,
  `elapsed_since_first_defer`, and `max_wait`; `abandoned` adds a bounded
  sample of 10. Labels are closed sets; no ARN, scope, or workload value is a
  label.

## 6a. Evidence

Performance Evidence: R2-F3 per-generation cost of one CAN_PERFORM intent
waiting on 10 missing targets, measured with
`go test -tags perf6785_wait ./internal/storage/postgres -run ReadinessWaitCost`
(`readiness_wait_cost_perf_test.go`). Postgres 16 container on the dev host,
real `FactStore` and `iamcantargets.Store`, graph writer stubbed out on both
sides. Scope: 1 000 roles, 3 000 `aws_iam_permission` facts, 1 000 exact S3
targets (990 ready, 10 in an uncommitted s3 scope), 343 registered scopes in
the account. 60 evaluations per generation (30 min bound / 30 s `RetryDelay`).
Three runs each; the "before" run is the same harness at 00ba81ddc with a
no-op `configureWaitHandler`.

| | Before (00ba81ddc) | After |
| --- | --- | --- |
| First evaluation | 106-109 ms, defers, 0 edges | 134-144 ms, commits 990 edges, then defers |
| Evaluations 2-60 | 82.0-83.6 ms mean (full fact load each) | 1.85-1.92 ms mean, p95 at most 2.8 ms (poll) |
| Handler time per waiting generation | 4.95-5.04 s | 0.248-0.253 s |
| Next generation, same missing set | 75-83 ms and defers again; the whole cost repeats every generation | 113-130 ms, commits, succeeds (`settled_missing`); no poll |

The after first evaluation costs about 30 ms more because it extracts and
builds the 990 edge rows the before run never reached. Graph write time is
excluded on both sides; the after run adds one scope-wide commit per
generation, which `main` also paid.

Observability Evidence: `eshu_dp_reducer_readiness_waits_total{domain,outcome}`
gains `settled_missing`, and `abandoned` now fires once per missing set. The
handler tests `TestIAMCanPerformSettledMissingSetCommitsWithoutDeferring` and
`TestWorkloadCloudRelationshipInstanceWaitSettlesAcrossGenerations` assert the
counter values, and `TestReadinessWaitSurvivesSupersessionLive` asserts
`abandoned` = 1 on the real queue. `eshu_dp_iam_can_perform_cross_scope_targets_total`
is emitted only on committing evaluations
(`TestIAMCanPerformCrossScopeOutcomesOnlyOnCommit`). The wait log line carries
`elapsed_since_first_defer` against `max_wait`.

Live Postgres proofs (§5). These tests skip without `ESHU_POSTGRES_DSN`, so a
plain `go test` run does not exercise them. Run at 4443c4f6e against a
`postgres:18-alpine` container:

```text
ESHU_POSTGRES_DSN=… go test ./internal/storage/postgres -run ReadinessWaitSurvivesSupersession -count=1 -v
ESHU_POSTGRES_DSN=… go test ./internal/storage/postgres/readiness/wait/ -run Live -count=1 -v
```

- `TestReadinessWaitSurvivesSupersessionLive`: `supersession: N superseded at +6m, N+1 committed at first claim, settled at +10m (anchor from N); writes=2 retracts=2`, then `--- PASS (1.55s)`.
- `TestReadinessWaitConcurrentUpsertsKeepEarliestAnchorLive`: `--- PASS (0.10s)`.
- `TestReadinessWaitResetAnchorSettleAndClearLive`: `--- PASS (0.04s)`.

## 7. Cassette

- The role (prod and stage anchors) and the inline permission move to
  `aws:123456789012:us-east-1:iam` (the claim-region convention of the
  cassette's existing iam scope). The bucket moves to
  `aws:123456789012:us-east-1:s3`, replayed AFTER iam (the adverse order).
  Values stay synthetic.
- Expected B-7: CAN_PERFORM 1 (cross-scope) and USES 2. Whether a given run
  actually deferred depends on claim timing, so the unit tests cover both
  orders; a B-7 run cites the reducer's `readiness_wait_outcome` log line when
  it claims the defer fired (review P3-A).

## 8. Out of scope

- `crossscope.CheckProducerReadinessBeforeLoad` (`readiness_floor.go`) and the
  AWS runtime-drift gate (`awsCloudRuntimeDriftStatePendingMaxWait`) bound
  their waits with the same per-row anchor (`ReadinessCycleAnchor`), so they
  share the supersession hazard class R2-F1 found here. Whether their
  conditions can be persistent was not checked in this PR. The
  `reducer_readiness_waits` ledger gives them a fix path if so. Tracked in
  #6814.
