# Decision: terminal state for poisoned shared intents (#10069)

## Decision

Quarantine into `shared_projection_unroutable_intents` with reason
`unusable_delta_scope`, then complete the intent. No schema change: the table
(migration 098, the #5984 dead-letter mechanism) already exists, production
already wires the sink (`go/cmd/reducer/main.go`), and the insert is
`ON CONFLICT DO NOTHING` keyed on the intent id. This file explicitly records
that no column, table, or index was added, so `eshu-postgres-rigor` has no
surface here.

Rejected: an attempt budget or failed status (both need DDL on the hottest
queue table plus backfill semantics for in-flight rows, for a failure that is
provably deterministic on first sight); failing at the work-item layer (shared
intents are not work items -- there is no owning handler with a budget to
spend); doing nothing and fixing the comments only (leaves the infinite
retry and the cross-repo fence in place).

## Why quarantine is correct graph truth

The poisoned refresh owns a retract with no correct binding: the file-scoped
one has nothing to bind, the repo-wide one deletes every unchanged file's
edge with nothing to restore it (#6216). Completing without retracting keeps
unchanged files' edges (correct: nothing re-creates them) and lets the
generation's per-edge writes re-create the changed files' edges. Residual,
accepted: deleted-file edges stay stale until the next full generation, and
that staleness is now a queryable unroutable row instead of silent loss.

## Concurrency proof

Workflow stages per partition visit: claim lease → select batch → fence plan
→ retract → write → record unroutable → mark completed → release lease.
Shared state and conflict domains: pending/completed rows of one
`(domain, partition)` in `shared_projection_intents`; the `(domain,
partition)` lease; the intent-id-keyed unroutable row. Transaction scope is
one statement at a time (graph retract, graph write, unroutable insert, mark);
retry scope is the whole visit. Nothing new is claimed: the lease still
serializes a `(domain, partition)`, and the quarantine adds no lock, no
`FOR UPDATE` relocation (so no EvalPlanQual recheck applies), and no worker,
batch, timeout, or retry-count change -- useful concurrency is preserved
byte-identically on every path except the previously-always-failing one.

Bad interleavings closed: (a) two workers, same partition -- impossible, the
lease admits one; the loser records `PartitionsLeaseHeld` and does nothing.
(b) crash between unroutable insert and mark -- re-run re-records (no-op by
conflict) and re-marks (idempotent), while retract/write re-run idempotently.
(c) duplicate delivery of an already-quarantined row -- completed rows are
never re-selected, and a re-record is a conflict no-op. (d) a transient error
masquerading as poison -- quarantine requires the typed rejection through a
`%w` chain; text matches do not qualify, so backend faults keep retrying.
(e) typed rejection with no poisoned row under the shared predicate, or no
sink wired, or a failing sink write -- all fail the cycle with nothing
completed (fail closed).

Replay/retry matrix: duplicate delivery of succeeded work converges (no-op
re-record, idempotent mark); stale replay on a superseded generation still
drains as stale before reaching the retract; retries preserve transients
(`TestSharedPartitionRetriesUntypedRetractError`); dead-letter replay
completes once and never re-selects
(`TestSharedPartitionPoisonedRefreshDoesNotBlockPartition` asserts the second
cycle reports zero dead letters); concurrent workers on one conflict domain
stay lease-serialized; the empty queue still takes the idle path (no rows, no
split, no record). The shared predicate mirrors the writer's normalization
exactly (real-bool flag, `[]string` as-is, `[]any` stringified skipping `""`
and `"<nil>"`, blanks never count), pinned by `TestRowHasUnusableDeltaScope`
and by the writer-side `IsUnusableDeltaScopeError` assertion in the #6216
proof, so splitter and rejector agree row for row.

Theory proof (prove-the-theory-first): no performance theory is involved, and
the determinism theory needs no new shim -- the existing #6216 proof already
establishes that the rejection fires before any statement executes (zero
statements run), so the same payload rejects identically on every poll by
construction. That observation is what promotes this failure from "retryable"
to "terminal".

## Verification

Focused suites: `go test ./internal/reducer/sharedintent/
./internal/reducer/intents/shared/worker/ -count=1` green, plus the writer
retract/unusable-delta subset green. The new regression failed before the fix
(`retract edges: delta retract requires delta_file_paths`, zero intents
marked) and passes after. `git diff --check` clean, `gofmt`/`go vet` clean on
touched packages.

No-Regression Evidence: no Cypher text, statement shape, batching,
transaction, lease, worker-count, poll-interval, or selection-ordering change;
the healthy path executes the same statements in the same order with the same
batching. The only new work runs on the previously-always-failing path (one
predicate scan plus one bounded unroutable insert plus one reduced retract).
`TestSharedProjectionRunnerProcessesPendingIntents` and the full worker suite
prove the healthy path unchanged; the writer's #6216 proof still asserts zero
statements before rejection and now also the typed rejection.

Observability Evidence: the dead letter itself (queryable row in
`shared_projection_unroutable_intents` with the `unusable_delta_scope`
reason), the per-visit `UnusableDeltaDeadLettered` result field merged into
the cycle totals, and two structured log lines -- a WARN at quarantine time
(domain, quarantined count, sample intent and repository) and a WARN per
partition visit with a non-zero dead-letter count. Conflict domain:
`(domain, partition)` rows of `shared_projection_intents`; worker/lease
settings unchanged (8 partitions, 60s TTL, batch 100, 500ms poll doubling to
5s). Before: a poisoned partition completes 0 intents per cycle, errors every
poll, and healthy siblings never drain. After (regression test counts): cycle
one completes 3 intents with 1 dead letter and 0 errors; cycle two completes
the fenced per-edge row through the opened fence with 0 further dead letters.
A steady non-zero dead-letter count means a producer keeps emitting
unqualifiable delta scopes; the existing poll/backoff gauges keep reporting
blocking and retries around it. Deliberately not counted in
`eshu_dp_shared_edge_unroutable_rows_total`: that counter's contract is rows
a write could not route (partial/whole batch), and stretching its reason enum
from the worker side would blur it; the durable row plus the new log lines
are the signal.

## NOT_CHECKED / follow-ups

- The code-call projection runner (`reducer/code/call/projection`) builds its
  own retract rows and has no unroutable sink: its poisoned refresh still
  fails its acceptance unit every cycle. The typed rejection now propagates
  through its `%w` chain, so the same quarantine fits there once it wires a
  sink with its own proof. Its `refresh_dead_letter.go` comment still says
  "the partition fails and the intent dead-letters" -- true for that lane
  until the follow-up lands.
- No live-backend run: proof is unit-level (fake lease/reader/writer) plus the
  writer's executor-level fail-closed proof. The first production poisoned
  refresh will exercise the Postgres unroutable insert for real.
- Deleted-file edge staleness under a poisoned delta (above) is accepted
  residual, healed by the next full generation.
