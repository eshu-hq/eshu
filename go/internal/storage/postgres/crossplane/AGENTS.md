# AGENTS.md — Postgres Crossplane cross-scope redrive store guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../../AGENTS.md` for Postgres storage conventions.
3. `query.go` for the target-discovery fences, `state.go` for the
   claim/lease discipline, `ledger.go` for the already-satisfied ledger,
   `sweep.go` and `sweep_batch.go` for the live and catch-up paths.

## Invariants

- Keep the package clause as `package crossplanestore`; callers import the
  `storage/postgres/crossplane` path without an alias.
- Keep the lease fencing (`FOR UPDATE SKIP LOCKED`), claim/heartbeat/retry
  transitions, fencing-token completion, and the three discovery fences
  byte-identical when moving code; predicate or lifecycle changes need their
  own issue with queue proof per `eshu-postgres-rigor` (selection, claim,
  idempotency, stale-lease behavior — never serialize workers or shrink
  batches as the fix).
- Keep the target-discovery query and the completed outcome available to the
  external tests through `export_test.go`. Do not hand-copy the SQL into a
  test.
- Never import the parent `postgres` package from production code here:
  `cmd/projector`, `cmd/reducer`, and root `internal/storage/postgres`
  import this package directly, and importing back would be a cycle. Tests
  import it only for the `SQLDB`/`SQLQueryer` wrapper types,
  `ApplyBootstrap`, and the `ReducerQueue`/`FactStore` constructors.
- Shared test seed and assertion helpers live in this package's own
  external test files (Go test-only symbols do not cross package
  boundaries). Keep each helper's behavior identical to the moved original;
  the query-plan proof's seeds are shared by every live test in this
  package.

## Common changes

- Do NOT re-introduce an inline fan-out inside `ProjectorQueue.Ack`'s
  transaction for this feature — that design was proposed, reviewed, and
  rejected (stale-lease commit risk, unbounded transaction/lock hold time,
  missing telemetry). The durable shape is: a claim/completion marker table
  (`crossplane_satisfied_by_redrive_state`), a bounded keyset-paginated
  target-discovery query backed by a partial index
  (`fact_records_active_k8s_claim_redrive_idx`), and a sweep
  (`CrossplaneSatisfiedByRedriveSweeper.Sweep`) triggered AFTER Ack commits
  (`runCrossplaneRedriveHook`), never inside it. See `README.md` for the
  full design, fences, and evidence.
- The three fences (active-generation, active-claim, already-satisfied-for-
  this-identity) are load-bearing correctness, not defensive extras.
- The already-satisfied fence is `crossplane_satisfied_by_redrive_target_ledger`,
  keyed by (target scope, XRD group, XRD claim kind) — NOT by timestamp. A
  first review round shipped a timestamp-anchored fence
  (`fact_work_items.updated_at > xrd_fact.observed_at`) that looked correct but
  was silently ineffective: the XRD fact's own `observed_at` strictly advances
  on every resync of the platform repo even when its (group, claim_kind) is
  unchanged, so the fence never actually skipped anything across repeated
  syncs. Do not reintroduce a timestamp-based already-satisfied fence; keep it
  keyed on the stable (target scope, group, claim_kind) identity.
- **`RecordRedriven` MUST be called ONLY from
  `crossplane.CrossplaneSatisfiedByMaterializationHandler`, strictly after it
  commits a SATISFIED_BY edge — NEVER from the sweep's enqueue path.** A
  SECOND review round caught the sweep itself calling `RecordRedriven`
  immediately after `ReplayCrossplaneSatisfiedByMaterialization` (which is
  enqueue-only and returns before the reducer handler runs). Because
  auto-retry-on-dead-letter is disabled by default
  (`cmd/reducer/poison_liveness_wiring.go`), an intent that enqueues but later
  dead-letters would leave that design's ledger entry PERMANENTLY and
  SILENTLY marking the target satisfied — reopening the exact false-negative
  window #5476 exists to close, worse than the original bug because it is
  silent and permanent (the ledger PK never expires). Do not move
  `RecordRedriven` back into the sweep.
- `Sweep` is the live post-Ack trigger path; `SweepBatch` is the
  startup/periodic catch-up path that reclaims stale/expired claims and
  re-runs the fan-out. **Both must stay wired.** `Sweep` alone cannot recover
  from a transient DB error or process crash mid-fan-out — nothing else
  revisits a `'claimed'` row once its lease expires unless a caller invokes
  `ClaimBatch`/`SweepBatch` periodically. `cmd/projector`'s
  `runCrossplaneRedriveCatchUpLoop` is that caller; do not remove it or the
  durable-resumability claim in this package's docs becomes false again.
- `ReplayCrossplaneSatisfiedByMaterialization` MUST reuse the exact same
  per-scope `EntityKey` (`"crossplane_satisfied_by_materialization:" +
  scopeID`) the projector's own trigger uses. A divergent per-XRD-generation
  identity would break the queue's natural dedupe and could double-process a
  target scope.
- `CrossplaneRedriveStateStore.MarkCompleted` is fenced by the bumped
  `claim_fencing_token` (mirroring `aws_freshness_triggers`, #4576), NOT by
  `claimed_by`. `Owner` is a static per-process-class string shared by every
  replica and the catch-up scanner (mirroring `ProjectorQueue`/`ReducerQueue`'s
  own `LeaseOwner` convention), so a `claimed_by`-only fence would let a stale
  invocation whose lease was reclaimed by another invocation under the SAME
  owner string silently complete a claim it no longer holds. Do not weaken
  this back to an owner-string fence.
- Operator dashboard decision: the three new counters
  (`eshu_dp_crossplane_redrive_sweeps_total`,
  `..._targets_enqueued_total`, `..._pages_processed_total`) are
  intentionally NOT added to
  `docs/public/observability/dashboards/eshu-operator-overview.json`.
  They are diagnostic sweep-throughput signals, not headline-alarm-worthy,
  consistent with the sibling `eshu_dp_crossplane_satisfied_by_edges_total`
  (#5347) and the identity-cache counters in the parent package's guidance,
  neither of which is on the dashboard either. An operator diagnoses redrive
  health
  through these counters directly (e.g. a Prometheus query or the structured
  `crossplane redrive catch-up sweep failed`/`completed` logs), not a
  dashboard panel. Revisit this decision if a production incident shows the
  redrive path silently stalling without an operator noticing in time.

## Failure modes

- Importing the parent `postgres` package from production code creates an
  import cycle.
- Duplicating (instead of exporting) shared production SQL or predicates
  lets the copies drift on locking or lifecycle behavior.
- Moving `RecordRedriven` into the sweep's enqueue path permanently and
  silently suppresses dead-lettered targets.
- Dropping `FOR UPDATE SKIP LOCKED` lets two concurrent claimants take the
  same XRD generation; dropping the fencing-token match lets a stale claimant
  complete a re-claimed sweep.

## Verification

From `go/`, run:

```bash
go test ./internal/storage/postgres/crossplane/... -race -count=1
go vet ./internal/storage/postgres/crossplane/...
```

Run `scripts/verify-package-docs.sh` from the repository root.
