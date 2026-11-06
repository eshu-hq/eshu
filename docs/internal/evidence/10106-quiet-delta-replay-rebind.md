# #10106: A Quiet Delta No Longer Drops a Scheduled Crossplane Replay

Root-Cause Evidence: the RED run of the `quiet_delta_replay` subtest of
`TestCrossplaneSatisfiedByRedriveClosesXRDLagWindowLive` on base
`ec1643d67f` exited 1 with all three interleavings failing for the
intended reason: `SATISFIED_BY rows after quiet delta = []` (pending
replay), `Execute status = "superseded" (generation claimed-app-full
superseded for scope claimed-app)` (claimed replay), and `concurrent
claim winners = 0` (concurrent claimers). Two drop points cause it. A
still-pending replay row is terminalized by the claim-time
inactive-generation sweep (`supersedeInactiveReducerGenerationsCTE`,
embedded in both the single and batch claim statements). An
already-claimed row is returned as superseded by `Runtime.execute`'s
`GenerationCheck` (wired at `cmd/reducer/main.go`) and acked without
running its handler. The quiet delta derives no replacement intent,
because the projector builds intents only from a generation's own facts
(`generationOwnedFacts` in `internal/projector/runtime/projection.go`).
This is the scheduling-to-execution window the #10094 evidence note
recorded and left open.

## Design

A Crossplane SATISFIED_BY intent is keyed per scope, not per generation:
its `EntityKey` is `"crossplane_satisfied_by_materialization:" + scopeID`,
its payload carries no per-fact data, and its handler materializes the
scope's active snapshot (`ListActiveCrossplaneSatisfiedByFacts` reads by
scope, delta overlaid on base; the retract is scope-scoped and the edge
MERGE is idempotent by claim/xrd uid). Running it under the successor
generation therefore computes exactly what the successor's own intent
would. The fix re-runs the superseded replay against the new active
state, in place, without serializing the sweep or the reducer:

- `supersedeInactiveReducerGenerationsPredicate` exempts
  `crossplane_satisfied_by_materialization`, so a pending replay stays
  claimable across a same-scope activation. The claim candidate CTEs
  carry no other generation filter, so no candidate change is needed.
- `Runtime.execute` rebinds a superseded intent of this one domain to
  the scope's active generation (`Runtime.ActiveGeneration`,
  `postgres.NewActiveGenerationID`: one `ingestion_scopes` primary-key
  probe) and re-checks `GenerationCheck` before dispatch. The handler
  writes under the successor generation; the result evidence summary
  records `rebound from superseded generation ... to active generation
  ... (#10106)`. `NewDefaultRuntime` threads the lookup from
  `DefaultHandlers.ActiveGeneration`; `cmd/reducer` wires it next to
  `GenerationCheck` (pinned by
  `TestBuildReducerServiceWiresActiveGenerationRebind`).
- Every other domain keeps the terminal superseded result: their
  intents carry generation-scoped facts, so rebinding them could write
  one generation's facts under another's identity.
- The rebind budget is bounded (`maxGenerationRebounds = 3`). Each
  rebound consumes a real activation that committed between two
  primary-key reads, so exhaustion means sustained activation churn; it
  returns `GenerationRebindExhaustedError`, retryable and classified
  `generation_activation_not_ready` (the enrolled non-counting class),
  so the queue re-runs the intent once a generation settles instead of
  dropping it.

Rejected alternatives: carrying debt by enqueueing a successor work
item at Ack time (needs the same exemption at claim time anyway, plus
batch-ack surgery on a second hot path, and a second claim cycle of
latency); deriving the intent from the delta's union base in the
projector (re-runs the full scope materialization on every quiet commit
to a claim-repository and breaks the "a union base row never derives an
intent" invariant every other domain relies on); serializing the sweep
against activations (forbidden by the issue and the root rules).

## Regression: RED then GREEN over the real queue and runtime

The `quiet_delta_replay` subtest of the scheduled
`TestCrossplaneSatisfiedByRedriveClosesXRDLagWindowLive`
(`go/internal/storage/postgres/crossplane/satisfied_by_quiet_delta_replay_live_test.go`)
runs the real sweeper, `ReducerQueue` (Claim/Ack), `NewDefaultRuntime`
with the Postgres freshness check and active-generation lookup, and the
real materialization handler over `FactStore`, on disposable PostgreSQL
18. Cases: (1) sweep schedules against the full generation, a quiet
delta activates, drain — the edge lands under the delta generation and
the full-generation work item ends `succeeded`, not `superseded`;
(2) the replay is claimed first, the delta activates, execute — the
handler runs against the delta snapshot; (3) two queues on independent
connections claim concurrently after the delta — exactly one winner
executes and writes the edge once. RED on `ec1643d67f` exited 1 (above);
GREEN at this change's head exits 0. The hermetic matrix
(`TestRuntimeExecuteRebind*` in `go/internal/reducer`) covers the
negative and boundary arms: other domains never rebind, a nil lookup
keeps the old superseded result, no-successor lookups keep it, lookup
errors propagate wrapped, and a churn chase exhausts to the retryable
error with the handler never called. The claim-shape pins in
`reducer_queue_supersession_test.go` assert the exemption text in both
the single and batch claim statements.

## Concurrency

Conflict domain: one scope's reducer rows share the `(scope, scope_id)`
conflict key, and the live-lease unique index allows at most one live
lease per key, so the predecessor item (rebound) and a successor item
the delta derived itself can never execute concurrently; they converge
sequentially through retract-then-MERGE on the same active snapshot.
Case (3) proves the exactly-one-winner property over two real
connections after the exemption. Transaction scope is unchanged (the
claim statement is still one statement; the exemption is one more
predicate in its lock step, rechecked by EvalPlanQual like the rest).
Retry scope: a rebound intent that fails in its handler fails and
retries exactly as an intent scheduled against that generation would;
the Ack still fences on the claimed row's own lease and
`last_attempt_at` (the IntentID never changes). No worker count, batch
size, page size, or lease setting changed; the sweep is untouched.

## Performance Evidence

No-Regression Evidence: the hot path is statement-identical. A
current-generation execute runs the same single `GenerationCheck` probe
as before; the rebind branch is reachable only after that probe reports
superseded for the one Crossplane domain, and then costs at most
`maxGenerationRebounds` extra pairs of primary-key probes
(`scope_generations`/`ingestion_scopes` by primary key) plus one final
lookup on exhaustion — work the dropped intent previously never did
because it produced no edge. The claim statement gains one
`stale.domain <> 'crossplane_satisfied_by_materialization'` comparison
inside the supersede lock step, which already filters `stale.domain`
per row; statement count, lock order (`work_item_id COLLATE "C"`),
indexes, and limits are unchanged, and exempt rows strictly reduce the
sweep's writes. `BenchmarkReducerQueueClaimDeepQueue` was run at this
head on disposable PostgreSQL 18 (exit 0, both depths claiming
successfully), but the shared host carried load average ~69 during the
run, so its absolute numbers are not quiet-host evidence and no
latency claim is made from them; a quiet-host before/after claim
measurement is NOT_CHECKED. The full
`internal/storage/postgres/crossplane` live package (including the
#10094 plan guard) passes unchanged at this head. NOT_CHECKED at
production scale or on a cold cache.

## Observability Evidence

Observability Evidence: no new metric. A rebound run is visible in the
existing reducer completion record — the Service logs it as `succeeded`
with the rebound generation, and the result evidence summary carries
`rebound from superseded generation <old> to active generation <new>
(#10106)` — and in the handler's existing completion log and
`eshu_dp_crossplane_satisfied_by_edges_total`. Churn exhaustion rides
the existing `eshu_dp_reducer_retry_surge_total` under
`failure_class="generation_activation_not_ready"`. An unwired
`ActiveGeneration` is indistinguishable from pre-#10106 behavior in
metrics (superseded outcomes), which is why the wiring is pinned by a
test rather than a signal.

## What remains

- Other reducer domains still drop a superseded intent whose successor
  generation derives no replacement. That is correct for
  generation-scoped intents today; a domain may opt into the same
  treatment only after proving its handler is a function of the active
  snapshot and its intent identity is generation-free.
- Status and drain observers hide unleased older-generation rows
  (`activeFactWorkItemsCTE`), so a pending rebound candidate is briefly
  invisible to drain reads until claimed. Pre-existing shape; it is
  claimable immediately, so the window is one claim cycle.
- A rebound intent whose successor item already dead-lettered still
  runs (the predecessor item is a distinct work item and may succeed);
  the dead-lettered successor remains operator-repair territory, as the
  sweep already treats it.
