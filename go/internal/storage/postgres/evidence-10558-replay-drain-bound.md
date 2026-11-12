# Evidence: bounded dead-letter drain keeps the Limit cap (#10558)

`go/internal/storage/postgres/projector_queue_sql.go` changed the limited
replay variant (`replayFailedWorkItemsBoundedTemplate`, the #3560/#3652 P3
dead-letter backlog drain) from an inlined bounded `IN (SELECT ... LIMIT $2
FOR UPDATE SKIP LOCKED)` subquery to a `candidates AS MATERIALIZED (...)`
CTE feeding `WHERE work_item_id IN (SELECT work_item_id FROM candidates)`.

Why: inlined into the UPDATE's `IN` list, the planner may re-execute the
bounded select per outer row (nested-loop semi join); each execution locks the
next still-terminal row, replaying past the Limit. The MATERIALIZED CTE
evaluates the bounded candidate set exactly once, so the bound stays a true
cap. `ORDER BY work_item_id` keeps forward progress deterministic and
`FOR UPDATE SKIP LOCKED` keeps concurrent drains from fighting over the same
rows (unchanged semantics, moved verbatim into the CTE).

No-Regression Evidence: `TestTerminalReopenWritersStampReopenedAtLive/bounded_dead-letter_drain`
seeds two `dead_letter` rows in one scope and replays with
`ReplayFilter{StageReducer, Limit: 1}`, asserting exactly the first row is
reopened and the second stays `dead_letter` untouched. Baseline (pre-fix tree
at `1144e4d5b4^`): FAIL, both rows reset to pending (2 rows replayed under
Limit 1), 1.25s. After (this branch): PASS, exactly 1 row replayed, 1.33s.
Backend for both runs: PostgreSQL 18.6 (Debian 18.6-1.pgdg12+2) on the local
`eshu-10561-pg` container (`postgres://eshu@localhost:15440/eshu`), command
`go test -count=1 -run
'TestTerminalReopenWritersStampReopenedAtLive/bounded'
./internal/storage/postgres/` with `ESHU_POSTGRES_DISPOSABLE=1` (exit 1
before, exit 0 after). Shape coverage stays green:
`TestRecoveryStoreReplayFailedWorkItemsLimitBoundsUpdateNotJustScan` and
`TestRecoveryStoreReplayFailedWorkItemsBoundedDrainPlacesPredicateAfterLimit`
assert the `LIMIT $2` bound, `FOR UPDATE SKIP LOCKED`, and `$3`-onwards
predicate placeholders; `TestRecoveryStoreReplayFailedWorkItemsUnlimitedUsesSimpleTemplate`
asserts the unlimited path still uses the simple template. Full
`go/internal/storage/postgres` package suite green after the change (27.4s).

No-Observability-Change: no new metric, span, or log line. Replay outcomes
remain visible through the existing `reopened_at` stamp and the domain
backlog age (`listDomainBacklogs`), both asserted by the live test above.
