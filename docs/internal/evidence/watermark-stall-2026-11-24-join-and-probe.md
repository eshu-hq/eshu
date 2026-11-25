# Watermark-stall join bound and single probe: hermetic evidence

Date: 2026-11-24. Lane: drive/watermark-stall-fix (coordinator rolls up;
deploy, live parity, and QA before/after stay in the coordinator's slot).

## Claim

Two exact-preserving fixes for the advisory-group winners watermark
trailing the wall clock by ~6.5h (dirty log 50k+ rows deep all day, keyed
ticks ~60-140s each, advisory waits parking behind
`winners_watermark_not_ready` until they dead-lettered as
`winners_watermark_stale`):

1. `addManifestDependencySupplyChainConsumption` appended one consumption
   row per matching (dependency, affected package) pair with no bound, so
   a batch-scale intent retained tens of thousands of rows for one
   package. The join now streams one best row per (package, repository,
   observed version): the only rows the downstream grouping, split,
   representative pick, and repository set ever read.
2. Every keyed tick ran the five-tuple lateral probe twice (once to build
   the keyed active_facts, once to count the pre-filter scanned copies).
   The build now persists the probe to a narrow temp table and the count
   reads it: one probe pass per tick, same rows, same snapshot.

## No-Regression Evidence:

- Backend: go1.26.6 linux/arm64, hermetic unit tests plus the phase
  fake-transaction harness (no live DB in this lane).
- Producer baseline (unfixed code, 200 manifest deps x 200 affected
  packages sharing one package key): 42,000 rows retained for the
  package.
- Producer after: 9 rows (4 repositories x 2 versions + 1 versionless),
  each asserted to be the exact representative (preferred-row rule,
  versionless rule, blank-repository drop).
  `TestAddManifestDependencySupplyChainConsumptionBoundsFanout` was red
  before, green after.
- Producer common-shape cost
  (`BenchmarkAddManifestDependencySupplyChainConsumption`, 200 distinct
  packages, no fan-out): ~1.1 ms, 894 KiB, 9,887 allocs per op —
  the keyed index probe adds no regression on the shape without fan-out.
- Consumer baseline (unfixed code, scripted keyed tick): the lateral
  probe text executes in 2 statements per tick.
- Consumer after: 1 statement per tick, `ChunkScannedCopies` still 187
  and `ChunkKeptRows` still 1 on the same fixture.
  `TestKeyedTickScansIdentifierCopiesOnce` was red before, green after.
- Full suites green: `internal/reducer/supplychain/core`,
  `internal/reducer`, `internal/reducer/supplychain/...`,
  `cmd/reducer`, and the hermetic `internal/storage/postgres` suite
  (the only failure is the pre-existing environmental
  `TestSchemaBootstrapCoordinationDefaultsFitTheJobDeadline`, missing
  `ESHU_DEPLOY_CHECKOUT`, identical on a pristine base; host-load-gated
  live tests self-void without a quiet host).
- Field observations behind the fix (read-only QA replica + Mimir,
  2026-11-24 ~22:30Z, not experiment results): winners watermark age
  23,374 s on both engines; dirty-log depth 50-100k all day with oldest
  age ~6.5h; keyed `build_active_facts` phase mean 56 s; reducer heap
  16.6 GiB in use, 99.95% under `SupplyChainImpactHandler.Handle`
  (7.0 GiB `addManifestDependencySupplyChainConsumption`, 6.8 GiB
  `groupConsumptionRowsByRepository`).
- Why safe: findings are unchanged by construction (same probed pairs in
  the same order, same per-group representative, same versionless and
  blank-repository rules); the winner differential is set-based, so temp
  row order cannot move it; the winners contract hash is re-pinned under
  the same version with this rationale recorded in the pin comment.
  Live parity (`TestWinnersKeyedFanoutMatchesRecomputeLive`, keyed
  chunked/horizon live tests, staged-audit sweep) and the QA
  before/after (tick-phase timing, scanned-copies rate, dirty-depth
  derivative, watermark age) are NOT_CHECKED here and belong to the
  coordinator's live slot.

## No-Observability-Change:

No new metric, span, log line, label, queue, worker, lease, or knob.
`ChunkScannedCopies`, the tick-phase histogram, resweep counters, the
watermark gauge, and the waiting-wake counters keep their names, labels,
and meaning; the count now reads persisted rows instead of re-probing.
