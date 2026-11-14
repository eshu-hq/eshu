# 10643: cardinality-adaptive holder-read splitting

Issue #10643. Hot-CVE holder fan-out reads time out at the 10 s guard: a
5-id statement over ~67k candidate rows ran 9.8 s cold while the per-statement
bar is p95 under 2 s with max under 5 s. The H-3 covering indexes stay
unbuilt (disproved mechanism, see
`go/internal/storage/postgres/evidence-holder-covering-index-h3.md`); this
note records the splitting direction that answers the guard pressure instead.

## Mechanism

Each `fanOutHolderReadChunk`-sized chunk (5 ids) is partitioned by measured
per-id row mass before the holder read runs:

- one mass-probe statement per chunk (`holderMassProbeQuery` in
  `go/internal/storage/postgres/supply_chain_impact_fanout_mass.go`): per-id
  equality arms over the same serving partial indexes the shipped holder read
  uses, no scope join, no DISTINCT sort; one row per requested id, zero for no
  match. The probe overestimates by construction (superseded-generation rows,
  rows matching two arms) and every bias points at splitting, which is the
  safe direction.
- an id over 10,000 measured rows runs alone; cold ids pack at most 5 ids and
  10,000 combined rows per statement. No statement of two or more ids covers
  more than the threshold, so the 67k-row timed-out shape cannot re-form.
- the statements run with up to 4 concurrent reads under the same 10 s guard
  per statement (`fanOutHolderReadParallelism`); the cold common case still
  runs one direct call with no goroutines.

Splitting changes statement count only: the admitted id set and the
per-statement timeout are unchanged (the probe adds one statement per chunk
whose heap cost at production mass is unmeasured, bounded by the same guard
with fail-closed errors). No timeout, lease, worker-count, or
batch-size change; no DDL.

## Threshold and parallelism bound

- Mass threshold 10,000 rows: about 1/7 of the 67k-row statement that hit
  9.8 s cold at the same query shape, and a linear read of the single-hot-id
  shim worst case (~0.78 s) puts a 10k-row singleton near 0.5 s, 4x under the
  2 s per-statement bar.
- Parallelism bound 4: holder reads are I/O-read dominated (72% of exec in
  the production EXPLAIN behind `10564-chunk5-production.md`); one chunk
  splits into at most 5 statements, so 4 drains a fully split chunk in about
  two waves while one pass holds at most 4 read transactions.

## Before / after evidence

Production before (from `10564-chunk5-production.md` and the #10643 lane):
5 ids / ~67k candidate rows, verbatim holder query, 9,806 ms cold vs the
10 s guard; single-hot-id statements ~0.78 s worst case.

Fixture after (local PostgreSQL 18.6 disposable database,
`TestHolderAdaptiveSplitRowIdentityLive` /
`TestHolderMassProbeLive`, 5 scopes, ~40k background rows plus 2,000 hot-CVE
rows, 70 warm, 100 member, 6 cold, own-scope and tombstone edges):

- row identity: 3 holder scopes identical multi vs split-single union, with
  bidirectional EXCEPT 0/0 (`multi EXCEPT singles` and `singles EXCEPT
  multi`), both sides derived from the shipped
  `FindSupplyChainImpactHoldersQuery` constant (singleton side by parameter
  restriction, byte-prefix guard on the MATERIALIZED CTE head).
- timings (small fixture, local SSD — scale does not transfer, the invariant
  does): multi-id statement 3.7 ms, 5 singleton statements 7.6 ms total (max
  single 2.0 ms); mass probe 2.4 ms for 4 ids with exact masses
  (hot 2005 = 2000 holder + 5 own-scope rows, tombstoned rows excluded;
  warm 70; cold 2; unknown 0).
- concurrency: `TestAdaptiveSplitRunsStatementsWithBoundedParallelism`
  observes overlapping holder reads with peak never over the bound of 4.

The production claim is the invariant, not the fixture wall time: no
multi-id statement covers more than 10,000 measured rows, ~7x under the
observed 67k timeout mass, and every statement keeps its own 10 s guard.

## Telemetry

No new counter or label: the fan-out outcome set stays closed and per-id mass
is unbounded cardinality. Operators read `holder_statements`,
`split_chunks`, `hot_ids`, and `max_id_mass_rows` on the
`supply_chain_impact.fanout` span, the `supply chain impact holder fan-out`
summary log line, and the `fanout_holder_statements`, `fanout_hot_ids`, and
`fanout_max_id_mass_rows` result sub-signals. A failed probe or statement
counts `read_error`/`read_timeout` on the existing counter with the `holders`
site, exactly like a failed holder read today.

## No-regression evidence

- `go test ./internal/reducer/supplychain/...` green: the all-cold chunk
  still reaches the port as one statement per chunk
  (`TestAdaptiveSplitKeepsColdHandlerCallsWhole`), and the pre-existing
  chunking tests pass unchanged because a zero-mass estimate preserves the
  legacy call shape.
- `TestHolderCoveringPairIndexesStayUnbuilt` and
  `TestHolderCoveringExpressionBlocksIndexOnlyLive` green and their record
  updated for the new direction: no index ships.
- Row movement: none. No query text of the shipped holder read changed; the
  probe is a new read-only statement.

## Performance Evidence (#10643):

Baseline (read-only, QA primary database, PostgreSQL 18.6): the
verbatim holder query over 5 sampled hot CVE ids (67,064 candidate rows)
ran 9806 ms cold first run and ~560 ms warm; visibility map 95.4%. After
(shim shapes on the same backend, not shipped statements): N=5 cold
9806 ms becomes N=2 ~1159 ms (25,486 rows) and N=1 ~781 ms fresh id
(15,455 rows); generation_id-only proxy ~170 ms (3.3x over warm).
Fixture (disposable PostgreSQL 18.6, same values as measured below):
multi-id statement 3.7 ms (max single 2.0 ms); mass probe 2.4 ms;
bidirectional EXCEPT 0/0. Threshold 10,000 rows sits ~7x under the
observed 67k timeout mass; bound 4 drains a fully split 5-id chunk in
~2 waves. Terminal counts: admitted id set identical by construction
(union plus downstream sort); no query text of the shipped holder read
changed.

## Observability Evidence (#10643):

No new counter or label: per-chunk statements, splits, hot singletons,
and max mass ride the existing fan-out span attributes, the summary log
line, and the `fanout_holder_statements`, `fanout_hot_ids`, and
`fanout_max_id_mass_rows` result sub-signals. Probe/statement failures
count `read_error`/`read_timeout` on the existing counter with the
`holders` site, exactly like a failed holder read today — an operator
sees split activity, mass peaks, and failures on the surfaces they
already watch.

## Residual risk

A singleton hot id may exceed the threshold alone: splitting cannot help an
id whose own rows already blow the guard, and the probe's overestimate bias
means a cold id near superseded-row-heavy scopes splits more than strictly
needed (extra statements, each far under the guard). If a singleton still
times out, the next lane is keyset paging of that id's rows, not more
splitting and not a covering index (the H-3 mechanism disproof stands).
