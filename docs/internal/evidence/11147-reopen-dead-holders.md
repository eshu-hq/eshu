# #11147: reopen dead-lettered supply-chain impact holders

Issue #11147 (epic #10860; refs #10765, #9432, #11141, #10643) found that the
holder fan-out skipped a holder whose `supply_chain_impact` item was a dead
letter at the scope's active generation. The reopen statement matched only
`succeeded, claimed, running, pending, retrying`. A dead letter fell to the
enqueue path, whose `ON CONFLICT (work_item_id) DO NOTHING` insert reported
`pending_noop`. That holder kept its stale copy past the documented bound. The
ruling is recorded in
[the #11147 arbiter ruling](../arbiter/11147-dead-letter-holder-reopen-ruling-2026-11-21.md).

## What changed

- `reopenSupplyChainImpactHolderQuery` (now in
  `go/internal/storage/postgres/supply_chain_impact_fanout_reopen.go`) also
  locks `dead_letter` rows at the active generation. A dead letter whose class
  is not in `recovery.UnsafeReplayFailureClasses()` returns to `pending` with
  `attempt_count = 0`, lease, retry, and failure fields cleared, `visible_at`
  and `reopened_at` set to now, the identity authorization columns set as the
  admin replay sets them, the replay bit off, and `fanout_origin` removed. No
  new stamp is written, and `fanout_owed` and `fanout_remaining` stay.
- The unsafe-replay class set moved from `query/admin` (unexported, and not
  importable from storage: `query/admin` imports `storage/postgres`) to
  `go/internal/recovery/unsafe_replay_classes.go`. The admin endpoint reads it
  from there. There is still one source, and the admin tests that pin it to the
  projector and semantic-queue constants pass unchanged.
- The fan-out port method is now `ReopenHolderDetailed`, returning the outcome
  and the dead letter's prior class. The store keeps `ReopenHolder` as an
  outcome-only wrapper for its callers.

No winner statement changes, so the winners contract version is not bumped.
`git diff --stat 7c2948825d -- 'go/internal/storage/postgres/*winners*'
'go/internal/reducer/supplychain/core/*winner*'` is empty.

## Proof

Live PostgreSQL 18.6 (`pg-11082`, disposable template clones). The matrix and
regression first ran at commit `167a2dfd91` over base `7c2948825d`. After the
review fixes and the rebase onto `68284d80a2`, the seven dead-letter live
tests ran once more at `2e66606292` and all passed (exit 0; the admin-replay
race saw both orders, fan-out 4 and replay 4). Four of those tests were renamed
into the reducer-contention workflow's `TestReopenHolder` selector along the
way.

RED on base (the reopen statement unchanged, the port method only renamed):
`TestReopenHolderReopensActiveDeadLetterEveryClassLive`,
`TestReopenHolderDeadLetterNextClaimIsGenuineLive`,
`TestReopenHolderDeadLetterTwoConcurrentReopensMoveItOnceLive`, and
`TestReopenHolderDeadLetterInsideTheCallersTransactionLive` failed with
`reopen = {Outcome:pending_noop ...}, want dead_letter_reopened`. The
older-generation and admin-replay guards passed on base, as they should.

GREEN after the change (exit 0):

| Case (decision 6) | Test | Result |
|---|---|---|
| Dead letter at the active generation, each class | `TestReopenHolderReopensActiveDeadLetterEveryClassLive` | 7 reopenable classes (fan-out failed, abandoned at shutdown, handler timeout, retry exhausted, dependency unavailable, timeout, NULL) reopen genuinely; every unsafe class is skipped and the row is byte-identical |
| Dead letter at an older generation | `TestReopenHolderLeavesOlderGenerationDeadLetterLive` | untouched; the active item is enqueued or reopened |
| Owed and remaining kept, stamp removed, next claim genuine | `TestReopenHolderDeadLetterNextClaimIsGenuineLive` | claim at attempt 1, no `fanout_origin`, owed and both remaining holders present; a second fan-out while queued is `pending_noop` and adds no stamp |
| Two concurrent reopens | `TestReopenHolderDeadLetterTwoConcurrentReopensMoveItOnceLive` | both racers blocked on one row lock, then one `dead_letter_reopened`, one `pending_noop` |
| Concurrent admin replay | `TestReopenHolderDeadLetterRacesTheAdminReplayLive` | 8 races per run, exactly one mover each, both orders seen (fan-out 2 and replay 6, then 5 and 3 on the rerun); a replay event exists only when the replay moved the row |
| In-transaction caller (#10055) | `TestReopenHolderDeadLetterInsideTheCallersTransactionLive` | invisible before commit; reopened after commit; unchanged after rollback |
| Gauge reading | `TestSupplyChainImpactActiveDeadLettersCountsByClassLive` | counts by class with the owing subset; older generation, other domain, and live items excluded; dropping the active-generation join turns it red |

**Not fixed by this PR:** the flag-then-dead-letter case (a running holder
flagged by a fan-out, which then dead-letters), tracked in #11159. It is RED: the row ends `dead_letter` with
`cross_scope_replay_required = false` and class `projection_bug`. The flag is
cleared by the generic reducer dead-letter statement and by the migration 093
trigger, neither of which is local to this domain. The ruling for #11159
fixes it in the dead-letter statement, with no migration, in its own PR after
this one. Its test is not committed here.

Regression: the 95 live tests in files that use the fan-out store ran on the
branch. 77 passed, and 2 skipped because they need `ESHU_FLEET_GATE_PROOF_DSN`
(NOT_CHECKED). 15 failed only on `template_clone.go:435 ... the clone drop
failed`, after their assertions passed, at load average 37 to 48 (an
environmental cleanup failure; the clones were then dropped by hand). One,
`TestSuppressionFanOutCappedContinuationSurvivesNewerGenerationLive`, hit its
400 s context deadline in the serial first drain. It failed the same way on
base `7c2948825d` at load 48 (`m113` reached, against `m181` on the branch),
so it is load-gated and NOT_CHECKED, not a regression.

Unit: `go test ./internal/recovery ./internal/query/admin/... ./internal/telemetry
./cmd/reducer ./internal/reducer/supplychain/core ./internal/reducer
./internal/query/search/unscoped` passed. The unscoped route goldens did not
change, because the new gauges are registered only by `cmd/reducer` and observe
nothing before the first snapshot. A mutation that disables the cmd/reducer
wiring turns `TestPostgresBackedGaugesServeDeadLetterSnapshot` red.

## Performance Evidence:

Throwaway `EXPLAIN (ANALYZE, BUFFERS)` shim on a template clone seeded to the
QA shape: 10,000 OSV batch scopes with active and superseded generations,
210,001 `fact_work_items` rows (20,000 `supply_chain_impact` rows, 250 of them
active-generation dead letters with the QA class mix, plus 190,000 rows in five
other domains), analyzed. Each statement ran three times in `BEGIN` and
`ROLLBACK`. These are the third-run timings:

| Statement, holder state | Base | Branch |
|---|---|---|
| Reopen, dead-letter holder | 0.112 ms (no row moved) | 0.301 ms (row moved) |
| Reopen, succeeded holder | 0.258 ms | 0.223 ms |
| Reopen, pending holder | 0.106 ms | 0.092 ms |
| Gauge read | n/a | 2.049 ms, 457 shared hits |

The reopen plan is the same on both: an `ingestion_scopes_pkey` index scan
nested with a `BitmapAnd` of `fact_work_items_generation_idx` and
`fact_work_items_scope_generation_idx` (`scope_id` plus the status list). The
added `dead_letter` status rides the same index condition, and the row lock
count is unchanged (one row). The dead-letter case costs one row update that
base never made, which is the fix. The gauge read uses
`fact_work_items_stage_domain_status_idx` for the 250 dead letters and hashes
`ingestion_scopes`. It runs once per snapshot refresh (default 5 minutes), never
per scrape or claim.

The new cost per reopen is bounded by the change rate times the attempt budget:
a dead letter that fails again uses its fresh budget, and there is no cap across
reopens, per the ruling.

## Observability Evidence:

- `eshu_dp_supply_chain_impact_fanout_total{outcome}` gains
  `dead_letter_reopened` and `dead_letter_skipped` (each with `failure_class`,
  the prior class, written per holder) and `no_active_generation`.
  `pending_noop` now means only a queued or already-flagged row.
  `TestFanOutDeadLetterOutcomesCarryPriorClass` pins the series.
- Each reopened or skipped dead letter writes one info line, "supply chain
  impact dead-lettered holder reopened" or "... left dead: unsafe-replay failure
  class", with `holder_scope_id` and `prior_failure_class`. The pass summary
  line gains `dead_letter_reopened`, `dead_letter_skipped`, and
  `no_active_generation` counts, and the sub-signals gain
  `fanout_dead_letter_reopened` and `fanout_dead_letter_skipped`.
- New gauges `eshu_dp_supply_chain_impact_active_dead_letters{failure_class}`
  and `eshu_dp_supply_chain_impact_active_dead_letters_owing_fanout{failure_class}`
  are served from the reducer's background Postgres snapshot. They show no
  series before the first reading and none for a class with no dead letters. The
  alert is the owing gauge above zero for 30 minutes. They are documented in
  `docs/public/reference/telemetry/metrics.md` and the reducer-core coverage
  table.

## Not checked

- The flag-then-dead-letter fix (#11159, its own PR).
- A second residual, already on main and not introduced here: a genuine pass
  flagged while running that then fails in a non-counting class retries at
  attempt 1 with the origin stamp and runs as a one hop (#11173, its own PR).
  Reopened dead letters enlarge the population exposed to it.
- `TestSuppressionFanOutCappedContinuationSurvivesNewerGenerationLive` (load-gated
  on base and branch alike).
- The race detector on the new live tests (`-race` was not run on the loaded
  host).
- QA after deploy (decision 6): both sample keys showing current evidence and the
  decision-4 census.
