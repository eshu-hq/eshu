# Postgres Crossplane cross-scope redrive store

## Purpose

#5347 shipped the Crossplane Claim -> XRD `SATISFIED_BY` correlation ungated
within a repo, but left a cross-scope false-negative window open: if the XRD
platform repo is ingested for the first time AFTER a Claim repo's latest
generation, the Claim's own materialization pass runs with zero XRD facts
visible and produces no edge — a false negative that, for a Claim repo that
stops changing, never self-heals. This package closes that window with a
durable, bounded, paged sweep — deliberately NOT an inline fan-out inside
`ProjectorQueue.Ack`'s transaction (a rejected earlier design: unbounded
target discovery inside a fixed six-statement generation-activation
transaction risks stale-lease commits and unbounded lock hold time).

## Ownership boundary

This package owns the sweep's target-discovery query, the claim/completion
state and already-satisfied ledger statements, and the
`CrossplaneSatisfiedByRedriveSweeper`, `CrossplaneRedriveStateStore`, and
`CrossplaneRedriveTargetLedgerStore` types. The parent `postgres` package
keeps the schema bootstrap registry (`BootstrapDefinitions`) that the
`migrations/076_crossplane_satisfied_by_redrive_state.sql` migration
registers against, and keeps `ProjectorQueue.Ack`'s post-commit trigger
(`runCrossplaneRedriveHook`), which calls `Sweep`. `cmd/projector` owns the
write wiring: it constructs the sweeper and runs
`runCrossplaneRedriveCatchUpLoop`, the periodic caller of `SweepBatch`.
`cmd/reducer` constructs the target ledger store for the materialization
handler. `internal/reducer/crossplane` owns the
`CrossplaneSatisfiedByMaterializationHandler` that writes the ledger.

## Exported surface

- `CrossplaneSatisfiedByRedriveSweeper` with `Sweep`, `SweepBatch`,
  `CrossplaneRedriveSweepResult`, and `CrossplaneRedriveIntentReplayer`.
- `CrossplaneRedriveStateStore` with `NewCrossplaneRedriveStateStore`,
  `EnsureQueued`, `ClaimExact`/`ClaimBatch`, `MarkCompleted`, and
  `CrossplaneRedriveClaim`.
- `CrossplaneRedriveTargetLedgerStore` with
  `NewCrossplaneRedriveTargetLedgerStore` and `RecordRedriven`.

See `doc.go` for the godoc contract.

## Files

- `query.go` — the cross-scope target-scope discovery query
  (`listCrossplaneRedriveTargetScopesQuery`), keyset-paginated on `scope_id`,
  `LIMIT`-bounded.
- `state.go` — `CrossplaneRedriveStateStore`: `EnsureQueued`,
  `ClaimExact`/`ClaimBatch` (`FOR UPDATE SKIP LOCKED`, returning a bumped
  `claim_fencing_token`), `MarkCompleted` (fenced by that token, not
  `claimed_by`).
- `ledger.go` — `CrossplaneRedriveTargetLedgerStore.RecordRedriven`: durably
  marks a target scope already re-driven against one (group, claim_kind)
  identity. Called ONLY from
  `crossplane.CrossplaneSatisfiedByMaterializationHandler`, strictly after it
  commits a SATISFIED_BY edge — NEVER from the sweep. See the fence (3) entry
  below for why.
- `sweep.go`, `sweep_batch.go` —
  `CrossplaneSatisfiedByRedriveSweeper.Sweep` (the live post-Ack trigger path)
  and `.SweepBatch` (the startup/periodic catch-up recovery path); both share
  `runClaimedFanOut`.

## Dependencies

- `internal/storage/postgres/db` for the shared `ExecQueryer`/`Queryer`
  contracts.
- `internal/telemetry` for the sweep instruments.

## Fences

All three fences are required by the design and enforced in
`listCrossplaneRedriveTargetScopesQuery` / `sweepJoinKey`:

1. **Active-generation fence**: the target-discovery join requires
   `scope.active_generation_id = fact.generation_id`, and `sweepJoinKey`
   re-checks `isCrossplaneXRDGenerationStillActiveQuery` before every page —
   if the XRD's OWN generation is superseded mid-sweep, the sweep stops
   without resurrecting stale intents (the new generation's own activation
   enqueues its own fresh sweep).
2. **Active-claim fence**: the same join restricts every Claim candidate to
   the TARGET scope's current active generation.
3. **Already-satisfied-for-this-identity fence**: a `NOT EXISTS` subquery
   against `crossplane_satisfied_by_redrive_target_ledger` skips a target
   scope already recorded for this EXACT (group, claim_kind) identity.
   **This fence was originally shipped keyed on a timestamp**
   (`fact_work_items.updated_at > xrd_fact.observed_at`, "already ran with
   this exact XRD visible") **and was found ineffective in review**: an XRD
   fact's own `observed_at` is threaded per-generation at ingestion time
   (`git_content_fact_envelopes.go`/`git_source_processing.go`) and therefore
   strictly advances on every resync of the platform repo even when the
   XRD's (group, claim_kind) is completely unchanged, so `updated_at > $5`
   was false for every previously-satisfied target on every subsequent sync —
   the fence never actually skipped anything.

   The ledger table that replaced it went through TWO iterations of its own.
   **Iteration 1 (also rejected in review) had the SWEEP write the ledger
   entry immediately after `ReplayCrossplaneSatisfiedByMaterialization`
   returned.** That call is enqueue-only — it returns as soon as the work
   item is durably queued, strictly BEFORE the reducer handler runs. Because
   reducer auto-retry-on-dead-letter is disabled by default
   (`cmd/reducer/poison_liveness_wiring.go`), an intent that enqueued
   successfully but later dead-lettered (a handler bug, or an infra outage
   exceeding the retry budget) would leave the ledger PERMANENTLY and
   SILENTLY marking that (target scope, XRD identity) satisfied — the
   ledger's primary key is not generation-scoped and never expires — reopening
   the exact false-negative window #5476 exists to close, worse than the
   original bug because it is silent and permanent.

   **The shipped design instead has
   `crossplane.CrossplaneSatisfiedByMaterializationHandler` write the ledger
   entry itself, strictly after `WriteCrossplaneSatisfiedByEdges` succeeds**
   (`recordRedriveLedger`, one entry per distinct `(claim_group, claim_kind)`
   among the rows it actually wrote an edge for). The sweep's `sweepJoinKey`
   no longer touches the ledger at all — it only enqueues/reopens. The window
   between enqueue and handler completion where a second sweep might
   re-enqueue the same target is safe (not a regression): the reopen-or-enqueue
   in `ReplayCrossplaneSatisfiedByMaterialization` is idempotent on the
   target's own per-scope `EntityKey`, so redundant re-enqueuing costs a
   no-op, never a duplicate materialization. Once a target scope has an
   actual edge committed for a (group, claim_kind) identity, re-running its
   materialization again for the SAME identity is a deterministic no-op (a
   pure function of that target's own unchanged facts plus the same
   active-XRD set), so skipping it forever is correct. A LATER, genuinely new
   Claim generation in the target scope needs no ledger check at all: its own
   projector-triggered SATISFIED_BY intent resolves directly against the
   already-active XRD.

Trigger: `ProjectorQueue.Ack` calls `runCrossplaneRedriveHook` AFTER its own
transaction commits (never inside it), which calls `Sweep`. The hook is a
no-op unless `CrossplaneRedrive` is wired; a hook failure is logged and never
fails Ack — the generation is already correctly activated by that point.

**Crash/error recovery requires the catch-up loop, not just the live hook.**
A `Sweep` call that errors partway through the paged fan-out (a transient DB
blip) or whose process crashes mid-sweep leaves
`crossplane_satisfied_by_redrive_state` `'claimed'` with an expiring lease
that nothing else ever rechecks on its own — the remaining un-swept target
scopes would otherwise be stuck forever, reproducing the exact unbounded
false-negative window #5476 exists to close, now triggered by crash/error
instead of ingestion order. `cmd/projector` runs
`runCrossplaneRedriveCatchUpLoop` as a background goroutine alongside the
main projector `Service.Run` loop (started in `main.go`, sharing its
shutdown context): every `crossplaneRedriveCatchUpInterval` tick it calls
`SweepBatch`, which reclaims up to `crossplaneRedriveDefaultCatchUpBatchSize`
`'queued'` or lease-expired rows via `ClaimBatch` and re-runs the fan-out for
each. A per-tick error is logged and the loop continues — an auxiliary
recovery pass must never take down the primary projector service. `main.go`
joins the catch-up goroutine via a `sync.WaitGroup` before it returns (and
therefore before its deferred `db.Close()` fires): defers run LIFO, so the
`wg.Wait()` defer, registered after `db.Close()`'s, runs first, ensuring a
tick that is mid-`SweepBatch` at shutdown finishes against a still-open `db`
instead of racing a closed one. `cmd/projector/crossplane_redrive_catchup_test.go`
proves the tick calls `SweepBatch`, swallows its error, and that the loop
returns promptly on context cancellation, all against a fake
`crossplaneRedriveBatchSweeper` (no real Postgres needed).

## Gotchas / invariants

- Non-test code does not import the parent `postgres` package, so root can
  import this leaf without a cycle. Tests import root only for the
  `SQLDB`/`SQLQueryer` wrapper types, `ApplyBootstrap`, and the
  `ReducerQueue`/`FactStore` constructors. The shared seed and assertion
  helpers live in this package's own external test files; the two private
  production symbols external tests need (the target-discovery query and the
  completed outcome) are exposed through `export_test.go`, never hand-copied.
- `RecordRedriven` MUST be called ONLY from
  `crossplane.CrossplaneSatisfiedByMaterializationHandler`, strictly after it
  commits a SATISFIED_BY edge — NEVER from the sweep's enqueue path.
- `ReplayCrossplaneSatisfiedByMaterialization` MUST reuse the exact same
  per-scope `EntityKey` (`"crossplane_satisfied_by_materialization:" +
  scopeID`) the projector's own trigger uses. A divergent per-XRD-generation
  identity would break the queue's natural dedupe and could double-process a
  target scope.
- `CrossplaneRedriveStateStore.MarkCompleted` is fenced by the bumped
  `claim_fencing_token`, NOT by `claimed_by`. `Owner` is a static
  per-process-class string shared by every replica and the catch-up scanner,
  so a `claimed_by`-only fence would let a stale invocation whose lease was
  reclaimed by another invocation under the SAME owner string silently
  complete a claim it no longer holds.

## Telemetry

Low-cardinality only, no per-scope/per-XRD label:
`eshu_dp_crossplane_redrive_sweeps_total` (by `outcome`:
`no_active_xrd`/`already_in_progress`/`completed`/`reclaimed_mid_sweep`/
`sweep_error`), `eshu_dp_crossplane_redrive_targets_enqueued_total`,
`eshu_dp_crossplane_redrive_pages_processed_total`.

No-Observability-Change: this move changes no statement, predicate, label,
or emission path; every counter above is produced by the same code at the
same call sites.

## Performance evidence

No-Regression Evidence: every statement moves byte-identically and every
method keeps its receiver, so the committed theory-proof
(`query_plan_live_test.go`, 4,000 target scopes x 50 `K8sResource` facts,
OLD `Parallel Seq Scan` 104.482 ms vs NEW partial-index scan 6.932 ms with
row-set equivalence 0/0) and the concurrency proof (two independent
connections racing `ClaimExact` converge to exactly one winner via
`FOR UPDATE SKIP LOCKED`) and the crash-recovery proof (fencing-token
completion, reclaim-after-expiry) hold unchanged. The live tests that prove
them move with the code.

## Related docs

- [Postgres storage](../README.md)
- [Shared database contracts](../../db/README.md)

## Verification

From `go/`, run `go test ./internal/storage/postgres/crossplane/... -count=1`
and `go vet ./internal/storage/postgres/crossplane/...`. From the repository
root, run `scripts/verify-package-docs.sh`.
