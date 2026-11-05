# Join-only barrier arrival: rate evidence and mutant kills (#10084)

`collector.Service.Run` level-gates the `AfterEmptyBatchDrained` escape on the
flag (the `AfterEmptyBatchDrained` read in `Service.Run`,
`go/internal/collector/service.go`), so every idle poll drains while
the flag is set. A shard with nothing to account for — never committed,
committed then idle, or committed then refused — arrives join-only (`false`)
at the deferred-maintenance barrier on every idle poll instead of falling
silent and stranding a peer in an open epoch.

Performance Evidence (#10084): baseline = the in-repo pgbench Cost Evidence
for the idle-path barrier transaction (`go/internal/storage/postgres/README.md`,
Postgres 18.4 throwaway container, one seeded completed-epoch row, the exact
four statements: 653 tx/s uncontested ceiling, p50 0.89 ms, p99 4.31 ms);
after = this head runs the byte-identical transaction once per idle poll per
shard (the `AfterEmptyBatchDrained` read in `Service.Run` and the
`Begin`-to-`Commit` transaction in
`RunDeferredRelationshipMaintenanceAfterShardDrain`,
`go/internal/storage/postgres/deferred_maintenance_barrier.go`), i.e.
N tx/s at the 1 s ingester poll — 30x–130x below the measured ceiling, about
1.5% steady-state duty cycle at 10 shards. Backend/version, input shape,
counts, and the reuse case are in F1 below.

No-Observability-Change (#10084): the join-only arrival reuses the existing
rate-limited `deferred maintenance barrier idle; no epoch to join` INFO log
and every existing metric, span, and log field; it adds no metric, span,
worker, queue, runtime knob, or status field.

## F1 — arrival-rate decision (measure first)

- **Decision:** accept one short barrier transaction per idle poll per shard.
- **Threshold:** steady-state arrival load must stay at least 10x below the
  measured saturation ceiling at the matching client count. Re-measure on a
  disposable Postgres before merge if the transaction text changes; it did
  not (see reuse case below).
- **Wall-clock deadline:** the throwaway probe shape below completes in under
  5 minutes; the orchestrator-owned disposable-Postgres-18 re-measurement and
  two-shard live run carry their own bound in the promotion plan.

**Rate (derived from code, this head):** each idle poll takes exactly one
barrier transaction — `Begin` (`deferred_maintenance_barrier.go:140`),
`acquireDeferredMaintenanceStateBarrier` (`:151`), the epoch
`SELECT ... FOR UPDATE` inside `ensureDeferredMaintenanceBarrierEpoch`
(`:155`), `Commit` on the no-epoch branch (`:165`) — because the lock is
acquired before the no-epoch early return. At `ingesterCollectorPollInterval`
= 1s (`go/cmd/ingester/wiring.go:39`, wired at `:274`), an N-shard fleet
adds exactly N barrier transactions per second of new load.

**Per-transaction cost (measured, reused):** the Cost Evidence section in
`go/internal/storage/postgres/README.md` ("recurring idle-path transaction;
measured") ran `pgbench` with exactly these four statements against
Postgres 18.4 on a throwaway container: 653 tx/s uncontended baseline
(p50 0.89 ms, p99 4.31 ms), 889–1,325 tx/s under 3–10-client saturation.
A 10-shard idle fleet needs 10 tx/s — 30x–130x below the measured ceiling —
about 15.3 ms busy per second, ~1.5% steady-state duty cycle.

**Reuse case (no re-run):** the measured transaction is byte-identical at this
head. `git diff ab6df73458..HEAD` on
`deferred_maintenance_barrier.go` touches only comments (verified: no
non-comment `+`/`-` lines); the four statements, their order, and the
commit-before-return shape are unchanged, so re-running the throwaway probe
would re-measure the same text. Re-running needs a disposable Postgres, which
is orchestrator-owned heavy proof, not executor-lane work.

**Remainder (orchestrator-owned, NOT checked here):** disposable-Postgres-18
re-measurement of the barrier-lock transaction at the final rebased head,
plus the live two-shard run showing a committing peer's epoch completing
while the other shard is refused-only (acceptance-criterion live proof), plus
observing the F5 shutdown path (terminal post-cancel idle poll draining with
a cancelled context) there.

## F4 — mutant kills (unit-level overlays, all reverted)

Overlays applied to `service.go` in the worktree, run against the
new/updated tests, then reverted (`git diff` on `service.go` is empty after
each revert). Focused command for all three:

`go test -count=1 -run
'TestServiceMixedLandedAndRefusedDrainCalls|TestServiceCommittedThenRefusedKeepsArrivingJoinOnly|TestServiceRunEmptyBatchEscape|TestServiceRunCallsEmptyBatchDrainHook|TestServiceRefusedCommit'
./internal/collector/`

| Overlay | Shape | Result |
|---|---|---|
| M1 | Escape re-gated on commit state (`emptyBatchEscape := s.AfterEmptyBatchDrained && !committedEverMutant`, latch set on first commit) — the pre-#10084 suppression | FAIL exit 1, 11 failures, incl. `TestServiceCommittedThenRefusedKeepsArrivingJoinOnly` (both subtests), `TestServiceRunEmptyBatchEscapeAddsJoinOnlyDrainsPerIdlePoll`, `TestServiceRefusedCommitIsNotADrainCommit`, `TestServiceMixedLandedAndRefusedDrainCalls` (3 subtests). KILLED. Log: `/home/ubuntu/.cache/m1.log` |
| M2 | Forced `true` on refused drain (`!refused \|\| true` at `service.go:301`) | FAIL exit 1, 7 failures, incl. `TestServiceRefusedCommitOpensNoEpochAfterStartup/refused` (refused-must-never-report-true pin), `TestServiceCommittedThenRefusedKeepsArrivingJoinOnly` (both subtests), `TestServiceMixedLandedAndRefusedDrainCalls/landed,_drain,_refused`. KILLED. Log: `/home/ubuntu/.cache/m2.log` |
| M3 | Escape re-gated on drain-cleared commit state (`&& committedSinceDrain`) | FAIL exit 1, 17 failures, incl. both new/updated join-only tests, `TestServiceRunCallsEmptyBatchDrainHookOnEveryIdlePollForANeverCommittingShard`, `TestServiceRefusedCommitOpensNoEpochAfterStartup`, `TestServiceRefusedCommitIsNotADrainCommit`, `TestServiceMixedLandedAndRefusedDrainCalls`. KILLED. Log: `/home/ubuntu/.cache/m3.log` |

Green baseline on the fixed tree (no overlays): the focused command above
passes, and the full `go test -count=1 ./internal/collector/` passes
(exit 0).
