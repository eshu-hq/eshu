# #9961 Phase 5 5.1: shard-scoped generations measurement

Refs #9961. Part of #9209. Gate: waited on #9880 (code side cleared —
core #10244 merged, satellite #57 merged); #9880 post-deploy QA still pending.

## Decision, threshold, deadline (stated before measuring)

**Decision.** Shard-scoped generations (plan §4.4: one full-replace
generation per dirty `cves/<year>/<NNNxxx>` shard) fit the reducer iff every
dirty shard's generation stays an order of magnitude under the per-intent
bounded-evidence budget and per-shard reducer accounting cost is negligible
against the daily cadence.

**Thresholds.** T1: a full shard (1,000 records → 1,000 `vulnerability.cve`
facts) uses <10% of `maxSupplyChainImpactEvidenceEnvelopesPerIntent`
(100,000). T2: p95 per-dirty-shard reducer accounting <100 ms in-memory on
representative data. T3: full-day delta replay wall-clock <10 min on
dev-class hardware. T4: no dirty shard trips
`supplyChainImpactTruncation.partial()` at measured volumes.

**Deadline.** Time-boxed single-session measurement; numbers frozen at this
note's commit. The thresholds above were fixed before the runs below.

## What was measured (commands, exit codes)

Base `origin/main` (`52e0cbae93`) plus this branch's substrate
(`go/internal/vulnsource/cvelist_shard.go`, pinned by
`cvelist_shard_test.go` and the core accounting test
`shard_scoped_generation_accounting_test.go`). Backend: hermetic Go suites,
no live backend touched; host `linux/arm64` (16 cpu).

```text
go test ./internal/vulnsource/ -run 'TestCVEListShard|TestCVEListDirty' -count=1
# exit 0 — shard mapping, dirty-shard derivation, per-shard fact counts

go test ./internal/reducer/supplychain/core/ -run 'TestShardScopedGeneration' -count=1
# exit 0 — full-shard budget share 0.010 (output of the failing-threshold probe below)

go test ./internal/reducer/supplychain/core/ -run '^$' \
  -bench 'BenchmarkScratchFullShardSelection' -benchtime=10x -count=3
# exit 0 — 792946 / 778913 / 760363 ns/op per full-shard selection (1,000 records,
# real selectAdvisoryProvenance path, NVD-deferred → cve_list severity)

go test ./internal/vulnsource/ -run '^$' \
  -bench 'BenchmarkScratchFullDayDeltaReplay' -benchtime=10x -count=3
# exit 0 — 168982 / 161151 / 162253 ns/op per 2,000-CVE delta replay
# (dirty-shard derivation plus one generation per dirty shard)

go test ./internal/vulnsource/ -run 'TestScratchDeltaCounts' -count=1 -v
# exit 0 (scratch, deleted after the run) — 2,000 spread CVEs → 2 dirty shards
# ([cves/2024/0xxx cves/2024/1xxx]); 2,000 CVEs across six years → 12 dirty shards
```

The `BenchmarkScratch*` / `TestScratchDeltaCounts` shims were temporary
worktree files, deleted before commit; the numbers above are their observed
output. Sensitivity check: tightening the core budget assertion to `< 0.005`
fails with `full-shard budget share = 0.010`, proving the test measures the
real budget rather than asserting a tautology.

## Facts per shard

| Shard case | Records | Facts (`vulnerability.cve`, 1:1) | Budget share |
| --- | --- | --- | --- |
| Full upstream shard (`<NNNxxx>`, ≤1,000 records) | 1,000 | 1,000 | 0.010 (T1 PASS, <0.10) |
| Observed partial shard | 37 | 37 | 0.0004 |
| Full shard + 10x expansion evidence | 1,000 + 10,000 | 11,000 charged | 0.11 of budget, still loads |

The registry admits exactly `vulnerability.cve` for the `cve_list` class, so
facts equal records. Collection-side note: the workflow admits at most 100
CVE IDs per `cve_list` claim (`maxVulnerabilityCVEListRequests`), so a full
shard collects in ≥10 claims of ≤100 facts each; the reducer-side per-shard
intent still sees at most 1,000 facts.

## Reducer cost per dirty shard

Full-shard provenance selection through the real `selectAdvisoryProvenance`
path (NVD-deferred CVEs resolving CNA severity from `cve_list`): ≈0.76–0.79
ms per 1,000-record shard (T2 PASS: ~100x under the 100 ms threshold).
Every record in the shard resolves `SeveritySource = cve_list`, so the
measured path is the path #9880 merged, not a stand-in.

## Replayed full day of deltas

2,000 modified CVEs (2x a heavy real-world CVE List delta day) replay to
2 dirty shards when concentrated and 12 when spread across six years, in
≈0.16 ms wall-clock including generation-plan construction (T3 PASS by six
orders of magnitude). Dirty-shard fan-in, not per-shard cost, bounds the
daily work — and it stays in the single digits to low teens.

## Per-shard intent vs bounded evidence load / truncation

Charging a full shard (1,000 envelopes) against a real default
`newSupplyChainImpactEvidenceBudget(0)` keeps loading (`charge` returns
true, `exhausted()` false); the zero `supplyChainImpactTruncation` is
neither `partial()` (upsert-only, no spurious retraction freeze) nor `any()`
(no suppression-candidate discard). T4 PASS: a per-shard intent cannot hit
the bounded load at measured volumes.

## Residual (not measured here)

No live Neo4j ran on this host, so no `EXPLAIN`/`PROFILE` of graph
projection at full-shard volume is attached (NOT_CHECKED). The change adds
no Cypher shape — no `MATCH`/`MERGE`/`UNWIND` was added or altered — and
projection for `cve_list` facts rides the existing source-agnostic writer
the `nvd`/`osv` facts already use, so graph cost scales with the fact volume
measured above (≤1,000 facts per shard, ≤~12k per heavy day). Recording a
Neo4j-backed projection profile at full-shard volume is recommended as a
5.2 acceptance criterion, not a reason to redo 5.1.

## Filing verdict

- **5.2 (CVE List baseline + `deltaLog.json` tail): FILE.** The 5.1 theory
  is measured: one generation per dirty shard fits the reducer on every
  threshold with 10–100x headroom. Gate the 5.2 issue on this note and carry
  the Neo4j projection profile above as a 5.2 acceptance criterion.
- **5.3 (OSV bulk per ecosystem): DO NOT FILE.** It needs 5.2's
  cursor-continuity proof first, per the phase gate.
- **5.4 (catalog read surfaces): DO NOT FILE.** It waits on the owner's
  catalog scope decision (arbiter ruling DEFER text) in addition to 5.2.

No-Regression Evidence: the committed substrate is pure accounting plus
tests — `CVEListShardForCVEID`, `CVEListDirtyShards`,
`CVEListShardGenerations` in `internal/vulnsource` (dependency-light:
`strings`/`sort`/`strconv` only, no new import direction), and two
contract tests. No Cypher, graph write, queue, lease, worker, batching, or
concurrency change. Focused suites green:
`go test ./internal/vulnsource/ ./internal/reducer/supplychain/core/
./internal/workflow/` (exit 0), `go vet` on touched packages (exit 0),
`git diff --check` clean.

No-Observability-Change: no new metric, span, log key, status, or runtime
knob. The thresholds above are acceptance numbers in this note, not shipped
signals. Operators diagnose shard generations through the existing
source-snapshot and readiness surfaces.
