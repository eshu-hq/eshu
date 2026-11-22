# #11203 fact id owner guard: cost, locking, and rollout evidence

Refs #11203 (epic #10860). Measured on a private `postgres:18` (PostgreSQL
18.6, aarch64), host load average 14 to 19 during the runs. Every number below
is a ratio or an exact seconds value from the committed tests; the host is
shared, so wall time is indicative and the plan is the gate.

## What changed on the hot path

`fact_records` is keyed on `fact_id` alone. `upsertFactBatchSuffix`'s
`DO UPDATE SET` list never sets `scope_id` or `generation_id`, so a write from
another scope or generation that passed the fencing and unchanged legs replaced
the owner row's payload while the row kept the owner's identity. The change:

1. Two owner legs in the conflict WHERE, right after the fencing predicate
   (`factOwnerGuardClause`): `fact_records.scope_id = EXCLUDED.scope_id AND
   fact_records.generation_id = EXCLUDED.generation_id`.
2. `selectUnchangedFactIDs` reads the stored scope and generation and splits
   RETURNING-absent ids three ways in the one statement that already ran
   (unchanged, held by another owner, fenced out). No new round trip.
3. `upsertStreamingFacts` returns a typed `*FactIDOwnerConflictError` at the
   first conflicting batch and `IngestionStore` refuses the whole generation
   through the existing commit-refusal channel. No skip-and-continue.

## Locking (PostgreSQL 18 documentation)

The INSERT page, `ON CONFLICT DO UPDATE`, `condition`, reads: "Only rows for
which this expression returns true will be updated, although all rows will be
locked when the ON CONFLICT DO UPDATE action is taken. Note that condition is
evaluated last, after a conflict has been identified as a candidate to
update." (https://www.postgresql.org/docs/18/sql-insert.html, fetched
2026-11-22, same major version as the 18.6 used here.) The statement already
had a WHERE, so the conflicting row is locked whether or not it is updated;
the owner legs change which rows are updated, not which are locked.

Two transactions racing one new id (`TestFactIDOwnerRaceTwoScopesLive`): the
second statement blocks on the first's uncommitted insert (the test polls
`pg_stat_activity` for `wait_event_type = 'Lock'` before letting the first
commit), and after the first commits it re-evaluates the
conflict WHERE against the committed row and skips it as foreign. Six rounds,
both orders, each scope wins three: the first committer owns the row and the
second gets the typed error. `TestFactIDOwnerRaceFreeForAllLive`: 40 ids, two
scopes each, no forced order: exactly one commit and one conflict per id, no
deadlock, one row per id.

## EXPLAIN (ANALYZE, BUFFERS), 500-row conflicting batch with changed content

`TestFactOwnerGuardExplainLive`, two runs per statement, alternating, each in
BEGIN/ROLLBACK. The pre statement is derived from the shipped constant by
removing the clause. Both plans: `Insert on fact_records`, `Conflict
Arbiter Indexes: fact_records_pkey`, `Conflicting Tuples: 500`, `Tuples
Inserted: 0`, one `Values Scan` child. No new node. Only the Conflict Filter
gains two equality terms.

| Statement | Execution time | Buffers shared hit | dirtied/written |
| --- | --- | --- | --- |
| pre, run 1 | 8.241 ms | 7043 | 18 / 18 |
| post, run 1 | 8.058 ms | 7837 | 17 / 17 |
| pre, run 2 | 8.291 ms | 8422 | 16 / 16 |
| post, run 2 | 8.154 ms | 8483 | 13 / 13 |

Buffer hits vary 7043 to 8483 across runs of the same plan (page and catalog
state), so the pre/post hit difference is run-to-run noise, not the guard.

## Four 500-row mixes, 15 paired runs, three repeats

`TestFactOwnerGuardCostMixesLive` times the full classification path (statement
plus, for RETURNING-absent rows, the discriminator) for pre and post in strict
alternation, order flipped each run, 2 unrecorded warm-up runs, 15 recorded
runs per shape per mix. Pre = the shipped statement with the clause removed
plus the frozen pre-#11203 discriminator (`ownerGuardPreDiscriminator`,
measurement only). Medians and min..max in exact seconds; the bar is "median
within 3% or inside the run-to-run spread".

| Mix | Repeat | pre median [min..max] | post median [min..max] | post/pre | within 3% | post median inside pre spread |
| --- | --- | --- | --- | --- | --- | --- |
| all new | 1 | 0.021616 [0.017283..0.037983] | 0.023149 [0.017451..0.037524] | 1.071 | no | yes |
| all new | 2 | 0.027450 [0.016195..0.056466] | 0.028794 [0.016936..0.048034] | 1.049 | no | yes |
| all new | 3 | 0.027728 [0.024045..0.033107] | 0.027934 [0.023882..0.032127] | 1.007 | yes | yes |
| all conflicting, changed | 1 | 0.021851 [0.016527..0.036858] | 0.022913 [0.016677..0.037378] | 1.049 | no | yes |
| all conflicting, changed | 2 | 0.030044 [0.023320..0.046150] | 0.029680 [0.020208..0.096467] | 0.988 | yes | yes |
| all conflicting, changed | 3 | 0.028453 [0.019357..0.269249] | 0.027253 [0.022325..0.043332] | 0.958 | no | yes |
| all identical replay | 1 | 0.018284 [0.015390..0.038070] | 0.018750 [0.015137..0.038268] | 1.025 | yes | yes |
| all identical replay | 2 | 0.017379 [0.014878..0.019666] | 0.015847 [0.012979..0.023467] | 0.912 | no | yes |
| all identical replay | 3 | 0.017789 [0.012226..0.037404] | 0.018807 [0.011027..0.026554] | 1.057 | no | yes |
| identical replay, 1 foreign in 500 | 1 | 0.017578 [0.010314..0.034802] | 0.016624 [0.012943..0.033725] | 0.946 | no | yes |
| identical replay, 1 foreign in 500 | 2 | 0.016397 [0.010779..0.021485] | 0.015658 [0.012074..0.019769] | 0.955 | no | yes |
| identical replay, 1 foreign in 500 | 3 | 0.022191 [0.018955..0.048167] | 0.022691 [0.020073..0.034042] | 1.023 | yes | yes |

All 12 cells have the post median inside the pre run spread. The ratios scatter
from 0.912 to 1.071 with no consistent sign per mix, including "all new", whose
inserts never evaluate the conflict WHERE (so its 1.007 to 1.071 is host noise).
Spread is wide (a 0.27 s outlier is in one pre run); the measurement cannot
prove a difference below roughly 10%, and the plan shows no extra work. The
1-foreign mix ends with the typed error on the post path (the measured
outcome) and with the foreign row admitted as unchanged on the pre path (the
defect being fixed). A first 7-run pass logged one 1.522 ratio on that mix with
a pre spread of 0.0089 to 0.0417 s, inside the spread; the 15-run repeats above
replaced it.

## Same-owner idempotency (unchanged)

`TestUpsertFactBatchSameScopeGenerationResaveStaysIdempotentLive`: the owning
scope and generation re-saving its own id lands changed content, an identical
replay leaves `xmin` unchanged, and both are admitted. The #10873 live matrix
(`unchanged_skip_*_live_test.go`) passes unchanged.

## Rollout proof on the built binary

Built `eshu-bootstrap-index` from this branch and ran it twice over a
filesystem corpus of 8 fixture repositories from `tests/fixtures/ecosystems`
(`go_comprehensive`, `python_comprehensive`, `terraform_comprehensive`,
`kubernetes_comprehensive`, `helm_argocd_platform`, `lib-common`,
`orders-api`, `cloudformation_comprehensive`), a private PostgreSQL 18.6 and a
private Neo4j, appending one file to every repository between the runs.

- Run 1: 10 scopes, 10 generations, 1051 facts, exit 0.
- Run 2: 18 generations (8 scopes now hold two), 2125 facts, exit 0, 8
  `upsert_facts` commit stages per run.
- `fact_id_owner_conflict` in either log: 0. ERROR lines: 0. `ingestion
  refresh skipped` lines: 0. `fact_records` rows whose (scope, generation) pair
  has no `scope_generations` row: 0.

The conflict counter is in-process and the binary exits, so the zero is read
from the absence of the one-per-refusal ERROR log and refresh-skip log, which
fire on the same path as every counter increment. This is the 8-repository
filesystem subset, not the 20-repository golden corpus and not the cassette
collectors; it settles those collectors only. Ids from any collector this run
did not reach are not covered.

Performance Evidence: #11203 adds two column equalities to the conflict WHERE of
`upsertFactBatchSuffix` and the stored scope and generation to the existing
RETURNING-absent classification statement (no new statement). On local
disposable PostgreSQL 18.6 the 500-row conflicting-batch plan is unchanged
(`fact_records_pkey` arbiter, no new node, execution 8.058 and 8.154 ms post
against 8.241 and 8.291 ms pre) and four 500-row mixes (all new, all
conflicting with changed content, all identical replay, one foreign row in 500)
over 15 paired runs and 3 repeats keep the post median inside the pre run spread
in all 12 cells (ratios 0.912 to 1.071), with no consistent direction
(`TestFactOwnerGuardCostMixesLive`, `TestFactOwnerGuardExplainLive`).

Observability Evidence: a refused generation is visible as the generation
outcome `skipped` with reason `fact_id_owner_conflict`
(`eshu_dp_ingestion_generation_outcomes_total`, `eshu_runtime_refresh_skipped_by_reason_total`),
the collector run outcome `refused`, `eshu_dp_fact_id_scope_conflicts_total{reason=scope|generation}`
(one increment per conflicting fact in the conflicting batch), and one
`fact_id_owner_conflict` ERROR log per refusal with the scope and generation
ids, the count, and up to 10 samples of fact id, fact kind, and the incoming and
stored owners, never a payload
(`TestCommitScopeGenerationRefusesFactIDOwnerConflictLive`,
`TestRecordFactIDOwnerConflictCountsByReasonAndLogsIdsOnly`). Documented in the
metrics reference, `generation-outcomes.md`, and the telemetry coverage
registry.

## Retry cadence of a refused generation

No hold. The claim completes and the next collection tick re-collects the
scope, hits the same collision, and refuses again with a fresh ERROR log until
the producer's id is fixed. The destructive-successor hold gauge
(`eshu_dp_ingestion_successor_held_stale_scopes`) is deliberately not reused: it
counts a different condition (a shrinking successor) and its consecutive-refusal
semantics would mislabel this one.

## Where the refusal shows in status

The refusal reason travels on the same commit-refusal channel as
`destructive_successor_refused`, with no reason allow-list in between:

- the collector run outcome `refused` (`telemetry.CollectorRunOutcomeRefused`)
  and the generation outcome `skipped` with reason `fact_id_owner_conflict`;
- `eshu_runtime_refresh_skipped_by_reason_total{reason="fact_id_owner_conflict"}`;
- for a vulnerability-intelligence scope, the source-state row
  (`Recorder.RecordClaimRefused` in
  `go/internal/collector/extensionhost/vulnerability/source_state.go`) stores
  the reason as `last_error_class` and marks the source partial, so readiness
  reports it incomplete while keeping the last success;
- the `fact_id_owner_conflict` ERROR log and the
  `eshu_dp_fact_id_scope_conflicts_total` counter named above.

Not verified in this change: a status or admin page outside these surfaces that
special-cases `destructive_successor_refused` by name. A search of
`go/internal` finds that string only in the refusal sites, the closed reason
set, `backfill/generation_guard.go`, and tests.

## Not covered here

The reducer fact writers (`factwrite` single, batch, versioned) and the search
document writer have the same gap and no owner guard (#11213, #11214); a
generation leg there could freeze a reducer id that is stable across
generations, so they need a census first. `reducerWorkItemID` is not injective
(`TestReducerWorkItemIDIsNotInjective`, #11215). The producer guard reads
`facts.Envelope` literals only (#11216).
