# Dec 6(ii) winners scanned-copies chunk budget evidence

Refs #10972. Arbiter ruling Dec 6(ii): the keyed tick admits the oldest-first
budgeted prefix of the post-expansion identifiers — at most 200 identifiers
and at most 10,000 pre-filter scanned copies per tick — instead of an
identifier-bounded but probe-unbounded chunk. The same change reverts the
TEMPORARY 50000->5000 squeeze (constant, comment, field doc together: the
over-cap is 50,000 again) and adds the Dec 7 statement-timeout guard (120 s
per keyed-tick transaction, distinct `statement_timeout` failure class;
lifted for the over-cap full fallback). No settle-semantics change, no
identifier/worker/batch/interval reduction, no new index, no migration.

## Performance Evidence:

Baseline (pre-fix binary, same fixture): the representative backlog (2
identifiers at 120 kept / 240 scanned copies each plus the operator NULL-tuple
identifier: 480 queued scanned copies) drains in ONE unbounded tick —
`Bounded=false`, `ChunkScannedCopies=480`, single tick total (RED run
`TestScannedBudgetPreFixRedCheckLive` on `origin/main a1b799b03a`, local
disposable PostgreSQL 18.6).

After (this commit, same fixture): the backlog drains in 3 bounded ticks of
240/240/0 scanned copies — every tick at or under the 240-copy test budget,
first tick `Bounded` with `ChunkTruncatedBy=scanned_copies`, last tick
drained, identifier conservation 3/3, kept 240/240, scanned 480/480, and the
drained table byte-equal to a fresh full recompute with the operator arm
seeded (`TestSupplyChainImpactWinnersResweepScannedBudgetBoundsTicksLive`
PASS in 8.39 s incl. seeding, `-race` green). A lone over-budget identifier
(240 scanned vs a 100-copy budget) still completes whole in one tick and the
tick stays Bounded while identifiers queue
(`TestSupplyChainImpactWinnersResweepScannedBudgetFirstIdentifierCompletesLive`
PASS in 1.75 s).

Budget sizing (Dec 5(a) theory proof, coordinator-measured, not re-run
here): narrow probe 132 s for 200 unordered identifiers (87,913 buffer reads,
heap fetch per scanned copy); active-facts on the oldest-first representative
chunk 44,874 kept rows in 54.5 s (~0.97 ms/kept fact at cold I/O); live keyed
ticks 150-260 s at 200 identifiers with 50-61k kept rows and scanned about
equal to kept. The 15 s per-tick bar over ~0.97 ms/kept fact admits order
10k kept facts per tick; live scanned ~= kept, so one 10,000-copy
scanned-copies budget bounds both heap terms (kept <= scanned always). A tick
may admit one identifier past the budget (the current identifier always
completes, never split); the tick log reports the actual scanned count, so the
overshoot stays visible.

Backend: local disposable PostgreSQL 18.6 (Ubuntu 18.6-0ubuntu0.26.04.1),
full 375-migration bootstrap per test. No migration, no index, no stored-probe
change: the whole-chunk probe SQL is byte-identical (the winners contract
hash is unchanged, `TestSupplyChainImpactWinnersContractHashPinnedToVersion`
green), and the budgeted per-identifier probes derive from the same probe
body.

NOT claimed here: QA-scale before/after EXPLAIN, rebuilt-binary per-phase
timings on the real backlog (`build_active_facts` p95 <= 15 s/tick), and the
1 h QA-inflow watermark proof are coordinator-owned follow-ups under the
ruling; the 120 s guard default is sized from the post-fix tick maximum and
the coordinator confirms and tunes it against the measured post-fix QA tick
maximum.

## No-Regression Evidence:

Full `Winners` family (`go test ./internal/storage/postgres/ -run
'Winners'`): ok, 558 s, zero failures — includes the replay/retry matrix
(replay, writer-txn replay, deadlock, contention, admission-order, fanout
parity, horizon, rekey, scale, purge, stale, hotkey, landing, audit sweep),
the re-targeted narrow/active-facts plan proofs (291 index searched once per
identifier on the narrow plan; active-facts and count plans touch no 291
index by design), and the DDL parity, contract-pin, phase-shape, and
probe-count suites. Full `go/internal/reducer/supplychain/core`: ok. Full
`go/internal/telemetry`: ok. `git diff --check` clean; `gofmt`/`go vet`
clean on all touched packages.

Two plan tests
(`TestSupplyChainImpactWinnersResweepKeyedProbePlanLive`,
`TestSupplyChainImpactWinnersKeyedFanoutLivenessLoopsBoundedLive`) were red
on the untouched base (verified on a scratch `origin/main` worktree: the
Nov-24 narrow persist moved the 291 probe out of active-facts, but the tests
still explained active-facts for it and never created the narrow table).
Repaired in this change by re-targeting each assertion to the persisted shape
— probe assertions on the narrow-build plan (index used, 50 searches, 50
copies, CTE narrow, no fact_records seq scan), join assertions on the
active-facts plan (no 291 index, no fact_records seq scan, live_gens CTE),
count assertions on the persisted-rows count (no 291 index, count == 50) —
with no assertion weakened: every pre-existing strength (exact search and
row counts, no-seq-scan, CTE shapes) is preserved at its new site.

## Observability Evidence:

`chunk_truncated_by` rides the existing resweep span attribute and commit
log line beside `chunk_scanned_copies` (`""` drained, `identifiers`,
`scanned_copies`, `identifiers+scanned_copies`), pinned by
`TestWinnersMaintainerScannedCopiesSpanAndLogFields`: a budget trip is
distinguishable from an identifier-bound trip in the tick log. The Dec 7
guard trips record `statement_timeout` on the tick's
`eshu_dp_supply_chain_impact_winners_write_conflicts_total` series (site
tick), pinned by `TestWinnersResweepTimeoutClass` (SQLSTATE 57014 incl.
wrapped shape; every other code classifies as nothing) and
`TestResweepWinnersKeyedPinsStatementTimeout` (pin is the transaction's
first statement). The `build_active_facts` phase histogram covers the
per-identifier probes with no new stage. Safe because admission stays inside
the existing transaction and single-flight lock (no new lock ordering: the
retry matrix above passes unchanged), settle uses the unchanged
chunk-restricted drain, and every admitted prefix is a whole-identifier
prefix proven byte-identical to the single-statement recompute.
