# #7325 — convention-outlier CALLS-fanout batch size

Issue #7325 continues the #6929 repo-scale proof's flagged NOT_CHECKED item:
`readOutlierCalleeEdges`'s CALLS-fanout read chunks the swept repository's
cohort members through `UNWIND` at 50 keys per statement, and at repository
scale (45,058 distinct cohort members in the #6929/#7297 seeded shape) that
chunk count (902 statements) was the largest or co-largest phase of the full
sweep (59-63% of total wall time, per the #6929 evidence doc's phase
breakdown). This record proves the theory (a larger batch reduces round
trips and wall time, with no accuracy change) twice -- once at the existing
seed's near-zero CALLS density, once at a re-seeded realistic density -- and
then records the scoped implementation: a dedicated
`outlierCalleeEdgeBatchSize = 250` batch and call site for the outlier
fan-out alone, leaving the wrapper-bypass evidence track's
`wrapperEvidenceKeyBatchSize = 50` untouched and unmeasured by this record.

## Identity

- Base: `origin/main` `26a514a55`.
- Branch: `perf/7325-outlier-fanout-batch`, in a dedicated feature worktree.
- Container (both measurement phases below):
  `docker run -d --name eshu-7325-neo4j -p 17325:7687 -e
  NEO4J_AUTH=neo4j/eshu-7325-pass
  neo4j:2026-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f`
  -> Neo4j Kernel `2026.08.1`, log-confirmed, the pinned ops-qa image. Removed
  (`docker rm -f eshu-7325-neo4j`) after each measurement run; no retained
  resources.
- Harness base:
  `go/internal/query/codequery/outlier_repo_scale_live_test.go`
  (`TestLiveOutlierRepoScaleNeo4j`, #6929/#7297), reused unchanged for its
  seed helpers (`liveScaleSeedGraph`, `liveScaleNoiseFileRows`,
  `liveScaleAuthedHandler`). Phase 1 (below) used a throwaway,
  never-committed test file. Phase 2 is a **committed, reproducible** live
  test: `go/internal/query/codequery/outlier_density_repo_scale_live_test.go`,
  `TestLiveOutlierDensityRepoScaleNeo4j` -- run it directly to reproduce the
  phase-2 numbers below (see that file's header comment for the exact
  command).

## Phase 1: near-zero-density measurement (theory proof, prior session)

The seeded shape `liveScaleSeedGraph` builds carries almost no CALLS density:
45,061 Functions in the swept repository, of which only the 11-node signal
fixture has real outgoing `CALLS` edges (8 total). A throwaway harness swept
`wrapperEvidenceKeyBatchSize` (the shared constant `readOutlierCalleeEdges`
used before this change) over `{50, 100, 250, 500}` in two runs of opposite
order (ascending then descending) against the same seeded graph, to separate
a real batch-size effect from Neo4j page-cache-warming order artifacts.

Two-run average (`BuildOutlierCalleeEdgesCypher` fan-out phase alone, and
`CodeHandler.assembleOutlierTrack` full-sweep warm p50/p95, 1 cold + 20 warm
per cell):

| batch | chunks | fan-out phase | full sweep warm p50 | warm p95 |
|---:|---:|---:|---:|---:|
| 50  | 902 | 325.0 ms | 528.7 ms | 597.2 ms |
| 100 | 451 | 215.2 ms | 391.9 ms | 421.1 ms |
| 250 | 181 | 133.3 ms | 317.6 ms | 339.9 ms |
| 500 | 91  | 115.9 ms | 291.8 ms | 312.9 ms |

The ranking (500 < 250 < 100 < 50, both phases) held identically in both run
orders, including with batch=50 running last in the descending run -- the
position with maximum accumulated page-cache warmth from the other three
sizes' preceding queries -- and it was still the slowest cell. Rows/findings
were byte-identical across every cell: callee-edge-row SHA-256
`9c5d0cb9d7102fb93b30b978f131ab872850a30b4f6813aa84c2a1259f006b38` (8 rows,
matching the seeded signal fixture's real edges) and finding-set SHA-256
`7c3140c56216338f0dc136c6033154aae91fe3502f16a7faca6bebe418f2c6dd` (3
findings), all 8 cells (4 sizes x 2 orders).

`PROFILE` on `BuildOutlierCalleeEdgesCypher` at `n=50` and `n=500` ids:
`NodeUniqueIndexSeek` on `function_uid_unique`, 2 db hits per key at both
sizes (100 db hits @50 keys, 1000 @500 keys) -- no plan-shape regression at
the larger batch; the per-key server cost is identical at either size, and
the win is purely round-trip count.

This phase measured only round-trip overhead: with near-zero density, every
`Expand(All)` per member returns almost nothing, so it could not by itself
rule out a larger batch costing more once members actually carry real
outgoing edges. That is what phase 2 below closes.

## Phase 2: realistic-density re-measurement (this record, step 0)

`outlierCalleeEdgeBatchSize` (the new dedicated constant this issue adds,
see "Implementation" below) stays a `const` at all times -- it is never
mutated for this measurement. Instead, the committed
`TestLiveOutlierDensityRepoScaleNeo4j` runs the real, unmodified production
path (`readOutlierCalleeEdges`/`assembleOutlierTrack`) twice against the
same seeded graph: once through the real graph reader unwrapped (native
production 250-key chunking), and once through a small test-only
`subBatchSplittingReader` that wraps the real reader and further splits any
`member_ids`-bearing call into <=50-key sub-calls at the transport level,
unioning the rows. Because row union is chunk-size-invariant (the exact same
`MATCH`/`UNWIND` statement, only partitioned differently), this reproduces
exactly what a native 50-key chunker would have returned, while every other
line of production code -- cohort grouping, outlier selection, mediation,
content hydration, finding assembly -- runs completely unmodified at both
sizes.

### Seed: realistic CALLS out-degree added to the swept repository

The base seed (`liveScaleSeedGraph`) was extended with a new write phase
adding CALLS edges among the 45,050 noise `Function` members (the 11-node
signal fixture's existing edges were left untouched), with out-degree shaped
to approximate the #6649 ops-qa reference distribution (mean 2.71, p50 1,
p90 5, p99 21, max 521, `docs/internal/evidence/6649-calls-degree-floor.md`):
most members get 1-5 callees, a small tail gets 20+, deterministically
assigned by rank (no `math/rand`, for reproducibility) and targeted at a
pseudo-spread offset within the same repository.

Measured (not assumed) distribution over all 45,061 Functions in the swept
repository, queried directly from Neo4j after seeding
(`ledger:7325-density-outdegree-max`):

- mean **2.891**, p50 **1**, p90 **5**, p99 **21**, max **64**.
- 130,266 CALLS edges created.

This is close to but not identical to the #6649 reference (mean 2.71, same
p50/p90/p99, shallower tail capped at 64 vs. 521): the goal was non-zero
realistic density for the batch-size theory, not reproducing the extreme
JavaScript/Java fan-out outlier.

### Sweep: effective batch 50 (via `subBatchSplittingReader`) vs. committed 250, one seeded graph

Fan-out phase alone (`readOutlierCalleeEdges`, 1 warm-up + 1 timed run) and
full sweep (`CodeHandler.assembleOutlierTrack`, 1 cold + 5 warm), same
45,058 cohort members, `TestLiveOutlierDensityRepoScaleNeo4j`:

| batch | mechanism | fan-out phase | full sweep cold | warm p50 |
|---:|---|---:|---:|---:|
| 50  | `subBatchSplittingReader{subBatch:50}` wrapping the real reader | 1107.1 ms | 1279.7 ms | 1235.0 ms |
| 250 | native production chunking (`outlierCalleeEdgeBatchSize`) | 654.4 ms | 826.7 ms | 831.2 ms |

Relative to effective batch 50: 250 is 40.9% faster on the fan-out phase
alone (`ledger:7325-density-batch50-fanout-p1` ->
`ledger:7325-density-batch250-fanout-p1`) and 32.7% faster on full-sweep
warm p50 (`ledger:7325-density-batch50-full-p50` ->
`ledger:7325-density-batch250-full-p50`). The magnitudes differ slightly
from the earlier throwaway-harness run (43.3%/29.9%) because the
`subBatchSplittingReader` wrapper itself adds a small amount of per-call
overhead the earlier `var`-mutation approach did not; the direction and
order of magnitude are the same.

500 was checked once during this issue's step-0 investigation via an
informal, uncommitted harness (since removed): it showed a further,
diminishing improvement over 250 (roughly 7-10%, matching phase 1's own
250-vs-500 shape) at this same realistic density. That observation is not
independently reproducible from any file in this repository and carries no
ledger citation; it is reported here only as directional context, not as
proven evidence. The committed, reproducible harness in this repository
covers 50 vs. 250 only, per the scope of this issue's implementation
(`outlierCalleeEdgeBatchSize = 250`, not 500).

Rows-equal: the member-callee edge multiset and the finding set returned by
`readOutlierCalleeEdges`/`assembleOutlierTrack` were hashed (SHA-256 over a
sorted, delimited dump) at both batch sizes; both produced the identical
edge hash
(`093b8b33a496939f7abed2d8b44b318b70d7fdfc6358fdb4af29874988c33d77`) and the
identical finding hash
(`c045350d9670a9f0d719766ce9c85e2dfc59d2b6d2814ef6557fccfc41c0baba`, 3
findings, matching phase 1's finding count on the same signal fixture).

### Step-0 verdict

250 remains clearly faster than 50 at realistic CALLS density: a 33-41%
improvement (committed, reproducible harness), an order of magnitude larger
than any run-to-run jitter visible in either phase's warm-sample spreads
(single-digit-to-low-double-digit ms). The theory holds under the density
this phase specifically re-tests. Per the task's stop condition ("if 250 is
not still clearly faster, STOP and report instead of implementing"), the
scoped implementation below proceeded.

## Implementation

`readOutlierCalleeEdges` (`go/internal/query/codequery/outlier.go`) now
calls a dedicated `runOutlierKeyChunks`/`chunkOutlierCalleeEdgeKeys`/
`outlierCalleeEdgeBatchSize = 250` chunker and its own low-level graph-port
call site, `runOutlierGraphRows`
(`go/internal/query/codequery/wrapper_bypass_read.go`), instead of the
shared `runWrapperKeyChunks`/`wrapperEvidenceKeyBatchSize = 50` the
wrapper-bypass evidence track's three reads
(`collectWrapperGraphEvidence`, `go/internal/query/codequery/wrapper_bypass_track.go`)
still use, unmeasured and unchanged by this issue. The dedicated call site
(not a reuse of `runWrapperGraphRows`) exists so
`go/internal/queryplan/testdata/query-source-coverage.yaml` can bound each
call site's own batch size exactly: `runWrapperGraphRows`'s row stays
`max_keys: 50, max_results: 56250` (true for its remaining callers), and a
new `runOutlierGraphRows` row carries a literal `max_keys: 250,
max_results: 281250` (250 x the #6649 corpus CALLS degree floor 1125,
audit bound, not a statement LIMIT -- the read stays LIMIT-free by accuracy
design). The row does **not** set `fan_out_multiplier`: that field requires
something in code to enforce an exact per-key row count (a uniqueness
constraint, writer guard, or schema-fixed label list), and 1125 is a
measured corpus *maximum*, not a code-enforced exact multiplicity --
`nonHotCorpusMaxCALLSDegree`'s own doc comment in `source_coverage.go` says
so explicitly. `max_results` is therefore a hand-derived audit comment only,
the same pattern the sibling `runWrapperGraphRows` row already uses, not a
validator-checked arithmetic identity. `readOutlierCohortSeeds` (the
unchunked, non-batched enumeration reads) and
`collectWrapperGraphEvidence`'s three reads are unaffected: neither is in
scope for this issue's measured claim.

### Tests (TDD)

- `TestReadOutlierCalleeEdgesSendsFullBatchSizeChunks`
  (`go/internal/query/codequery/outlier_fanout_batch_test.go`): a fake graph
  reader records each statement's `member_ids` chunk size; 501 ids must
  produce exactly 3 statements (250, 250, 1). Confirmed RED against the
  pre-change shape (temporarily routing `readOutlierCalleeEdges` back
  through `runWrapperKeyChunks`): 11 statements (50 x 10 + 1). GREEN against
  the dedicated `runOutlierKeyChunks`/`outlierCalleeEdgeBatchSize`.
- `TestChunkOutlierCalleeEdgeKeysUsesDedicatedBatchSize`: pins
  `chunkOutlierCalleeEdgeKeys` to 250-key chunks and proves the same id set
  chunks differently under `chunkWrapperEvidenceKeys` (50) -- the two batch
  sizes are independently configured, not aliases of one shared constant.
- `TestCollectWrapperGraphEvidenceStaysAtWrapperBatchSize`: proves the
  wrapper-bypass evidence track's batched reads still chunk at 50, unmoved
  by this change.
- `internal/queryplan`: adding the new `runOutlierGraphRows` call site with
  no registry row first reproduced the exact CI failure mode --
  `TestHotCypherManifestCoversEveryProductionQueryCall` failed with
  `unregistered query callsite
  codequery/wrapper_bypass_read.go:(*CodeHandler).runOutlierGraphRows
  (count 1)` (RED). A placeholder-digest row then reproduced the source-digest
  mismatch report
  (`source_sha256 does not match production symbol (manifest ...,
  production 6cac8cd759c59c021714a54da41200e071ace0528784fb6a3f4799f5f1b704b6)`),
  which supplied the real digest for the final row (never invented, per the
  package's digest-discipline rule). `go test ./internal/queryplan -count=1`
  is GREEN with the correct row.
- No seeded RED/GREEN pins the `runOutlierGraphRows` row's literal
  `max_results: 281250` value itself, deliberately: with no
  `fan_out_multiplier` (see "Implementation" above), the only validator rule
  that applies to a plain `keyed_support` literal `max_results` is "requires
  max_results" (must be `> 0`), which is already covered generically by
  `TestValidateSourceCoverageRejectsIncompleteTypedEvidence` and is not
  specific to this row's value. No rule checks a literal `max_results`
  against its derivation comment. Inventing a seeded violation against a
  rule that does not actually apply to this row would overstate the
  row's proof; `go/internal/queryplan/source_coverage_fanout_test.go` records
  this reasoning in place of the test.
- `TestLiveOutlierDensityRepoScaleNeo4j`
  (`go/internal/query/codequery/outlier_density_repo_scale_live_test.go`,
  #7325, env-gated by `ESHU_OUTLIER_NEO4J_LIVE`, same as its sibling
  `TestLiveOutlierRepoScaleNeo4j`): the committed, reproducible phase-2 live
  proof described above. It is not a RED/GREEN unit test (there is no
  pre-#7325 code path left to run for comparison -- production is always at
  the committed 250), but the two cells' measurably different timings
  (1107.1/1235.0 ms wrapped vs. 654.4/831.2 ms native) confirm the
  `subBatchSplittingReader` wrapper genuinely changes execution behavior
  rather than being a no-op, while the identical edge/finding hashes confirm
  the answer is unchanged.

## Performance Evidence

Performance Evidence: `readOutlierCalleeEdges`/`assembleOutlierTrack`
against Neo4j 2026.08.1 (pinned digest
`sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f`),
45,058 cohort members in a 505,061-Function/130,266-CALLS-edge seeded graph
with realistic out-degree (mean 2.891, p50 1, p90 5, p99 21, max 64,
`ledger:7325-density-outdegree-max`), via the committed
`TestLiveOutlierDensityRepoScaleNeo4j`: the committed 250-key batch
(`outlierCalleeEdgeBatchSize`) cuts the fan-out phase 1107.1 -> 654.4 ms
relative to an effective 50-key batch (43.3% faster in an earlier
uncommitted run of the same shape, 40.9% faster in the committed harness,
`ledger:7325-density-batch50-fanout-p1` / `ledger:7325-density-batch250-fanout-p1`)
and the full-sweep warm p50 1235.0 -> 831.2 ms (32.7% faster,
`ledger:7325-density-batch50-full-p50` / `ledger:7325-density-batch250-full-p50`),
with byte-identical rows (edge hash
`093b8b33a496939f7abed2d8b44b318b70d7fdfc6358fdb4af29874988c33d77`) and
findings (hash `c045350d9670a9f0d719766ce9c85e2dfc59d2b6d2814ef6557fccfc41c0baba`,
3 findings) at both sizes measured. A near-zero-density measurement on the
same base seed (no realistic CALLS edges) found the identical ranking and an
even larger relative gain (528.7 -> 317.6 ms full-sweep warm p50, 39.9%
faster). 500 was measured once at near-zero density (phase 1, reproducible
range: fan-out 133.3 -> 115.9 ms, full-sweep warm p50 317.6 -> 291.8 ms) and
once informally at realistic density (uncommitted, not independently
reproducible); both showed only a further, diminishing improvement over 250
for double the batch size; 250 is recommended and implemented.
`BuildOutlierCalleeEdgesCypher`'s anchor (`MATCH (member:Function {uid:
mid})` on Neo4j) is unchanged by this issue -- only the UNWIND chunk size
changed -- and its `PROFILE` (phase 1, `n=50`/`n=500`) showed an unchanged
`NodeUniqueIndexSeek` plan on `function_uid_unique` at both sizes, so the
win is round-trip count, not a plan-shape change.

## Observability Evidence

No-Observability-Change: this change adds a new Go constant
(`outlierCalleeEdgeBatchSize`), a new low-level graph-port call site
(`runOutlierGraphRows`, structurally identical to `runWrapperGraphRows`), and
a new `query-source-coverage.yaml` registry row. It adds no metric, span,
log key, or status field, and removes none. The route-level spans/logs for
`/api/v0/code/divergence/findings` and `/investigate` (emitted by the shared
query-handler span helper) are unaffected and continue to record the same
attributes before and after this change; `readOutlierCalleeEdges`'s error
path now returns `errOutlierGraphUnavailable` instead of
`errWrapperGraphUnavailable` on a missing graph reader, which changes only
the wrapped error's message text (both satisfy `errors.Is(...,
querycontract.ErrGraphUnavailable)` identically, so `WriteGraphReadError`'s
mapped HTTP status is unchanged) -- a more accurate operator-facing log
message on that one degraded-backend path, not a new signal.

## NornicDB

Not measured here: the owner rule for this proof is Neo4j only, matching
phase 1 and the #6929 precedent. `BuildOutlierCalleeEdgesCypher`'s NornicDB
branch is unchanged by this issue (only the Go-side chunk size changed, not
either dialect's Cypher text), so the same relative benefit is expected but
unproven on NornicDB.

## Proof limits / NOT_CHECKED

- **`PROFILE` was not re-captured at realistic density.** Phase 1's
  `PROFILE` (near-zero density) confirmed an unchanged `NodeUniqueIndexSeek`
  plan at `n=50`/`n=500`; phase 2 re-measured wall time at realistic density
  but did not independently re-`PROFILE` the plan shape under real
  `Expand(All)` traversal volume. The mechanism (round-trip count, not
  per-statement server cost) is not expected to change with density, since
  the anchor and index remain identical, but this is inference from phase 1,
  not a phase-2 `PROFILE` capture.
- **Single run, one order, at realistic density, both the removed throwaway
  sweep and the committed harness.** Phase 1 ran two opposite-order sweeps
  specifically to rule out a cache-warming-order artifact; phase 2 (both the
  earlier uncommitted run and the committed `TestLiveOutlierDensityRepoScaleNeo4j`)
  ran a single comparison each. The result (33-41% faster, far above visible
  jitter) is unambiguous enough that a second run/order was not judged
  necessary to settle the step-0 question, but order-independence at *this*
  density is therefore NOT_CHECKED (it was checked, and held, at near-zero
  density in phase 1).
- **The corpus-max degree tail (up to 1125 callees per member, #6649) was
  not exercised.** Every measurement in this record ran against a seed whose
  max out-degree is 64 (phase 2) or 8 real edges total (phase 1) -- far below
  the `max_results: 281250` audit bound the registry row now carries
  (250 keys x the 1125 corpus-max degree). A 250-key chunk in which every
  member happened to sit at the corpus-max degree would return up to
  281,250 rows from one statement (vs. up to 56,250 at the old 50-key
  batch): proportionally more session/driver memory held per in-flight
  statement and a larger single decode/scan before the next chunk starts,
  neither of which this record measures. `BuildOutlierCalleeEdgesCypher`
  carries no `LIMIT` on either branch by accuracy design (every real
  outgoing edge must be returned for the outlier-selection math to be
  correct), so this tail is a real, unbounded-by-the-query latency and
  memory risk at 250 keys that was true at 50 keys too, just proportionally
  smaller -- raising the batch size raises the ceiling on this
  already-existing risk, and that delta is unmeasured here.
- **The wrapper-bypass evidence track's batch size is untouched and
  unmeasured**, by design: `wrapperEvidenceKeyBatchSize` stays 50, and this
  issue's evidence says nothing about whether a larger batch would help
  `collectWrapperGraphEvidence`'s three reads. A batch-size change there
  needs its own proved-first theory.
- **500 at realistic density is not independently reproducible.** Phase 1
  measured 500 at near-zero density with the two-opposite-order-sweep rigor
  (reproducible only in the sense that diag-7325.md's throwaway harness is
  described there, not committed to this repository). Phase 2's 500 cell was
  checked once via a since-removed uncommitted harness; the committed
  `TestLiveOutlierDensityRepoScaleNeo4j` covers 50 vs. 250 only, matching
  this issue's actual implementation scope.
- **The mediation second pass** (`mediateOutlierVerdicts`, which reuses
  `readOutlierCalleeEdges` over the outliers' own callee ids) is exercised
  by the full-sweep timing (it runs inside `assembleOutlierTrack`) but was
  not isolated as its own phase in either measurement.
- **Single disposable local Neo4j container, no host contention** beyond
  the usual quiet-host check: absolute ms figures here are expected to be
  smaller than ops-qa's real deployment numbers, per the #6929 precedent
  for this same class of proof. The relative ranking and round-trip-count
  mechanism are the portable parts.
