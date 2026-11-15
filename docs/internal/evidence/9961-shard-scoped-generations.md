# #9961 Phase 5 5.1: shard-scoped generations measurement

**Update 2026-11-15 (#10671):** the "1:1 facts to records" accounting in this
note is wrong for the CVE List envelope, which emits one `vulnerability.cve` fact
plus one fact per distinct reference URL
(`cvelist_envelope.go:34-49`), so a shard's facts are records times 1 plus
references. The CVE List mirror is no longer the NVD fallback; see
[10671-nvd-feed-transport-fallback.md](../design/10671-nvd-feed-transport-fallback.md).
The thresholds here still describe a possible future catalog product.

Refs #9961. Part of #9209. Gate: waited on #9880 (code side cleared —
core #10244 merged, satellite #57 merged); #9880 post-deploy QA still pending.

## Decision, threshold, deadline (stated before measuring)

**Decision.** Shard-scoped generations (plan §4.4: one full-replace
generation per dirty `cves/<year>/<NNNxxx>` shard) fit the reducer iff every
dirty shard's generation stays an order of magnitude under the per-intent
bounded-evidence budget and per-shard reducer accounting cost is negligible
against the daily cadence. (Restated below: the budget does not charge a
shard's base load, so "under the budget" is a hypothetical bar; see the T1
restatement.)

**Thresholds.** T1: a full shard (1,000 records and their facts) uses <10% of
`maxSupplyChainImpactEvidenceEnvelopesPerIntent` (100,000); see the T1
restatement below for the corrected fact arithmetic. T2: p95 per-dirty-shard reducer accounting <100 ms in-memory on
representative data. T3: full-day delta replay wall-clock <10 min on
dev-class hardware. T4: no dirty shard trips
`supplyChainImpactTruncation.partial()` at measured volumes.

**T1 restated (correction 2026-11-15, #10718).** The first draft of T1 counted
one fact per record. The CVE List envelope emits one `vulnerability.cve` fact
per record plus one `vulnerability.reference` fact per distinct reference URL
(`CVEListRecordEnvelopes` in the collector satellite, `cvelist_envelope.go`
lines 34-49), so a shard's facts are records times 1 plus references. For a
full shard of 1,000 records, 10% of the budget is 10,000 facts, which leaves
room for 9,000 facts beyond the records themselves.

The per-intent budget does not charge a shard's own facts at all. It counts
expansion evidence only: the envelopes the evidence stages add beyond the
intent scope's own base load, which `LoadFactsForKinds` reads uncapped
(`evidence_truncation.go` and `evidence_load.go` in the core package). A
shard-scoped intent's `vulnerability.cve` facts are that base load, so the
0.010 share used below is a hypothetical headroom bar ("what a shard-sized load
would use if the base load were charged"), not budget use the reducer performs.
No reducer-side count or byte bound applies to a shard's own base load. The
payload-bytes cap (`maxSupplyChainImpactEvidencePayloadBytesPerIntent`) covers
expansion evidence only. The one live bound is the satellite's per-claim
10,000-fact write cap, which applies per claim, not per shard generation, and
which the design note `10671-nvd-feed-transport-fallback.md` already says a full
shard can exceed as a non-retryable error.

References add write volume, not reducer load: `supplyChainImpactFactKinds()`
(`active_filter.go`) does not list `vulnerability.reference`, and the kind is
registered read-only with no reducer domain decoding it, so rows land in
`fact_records` and count against the per-claim cap only. If `vulnerability.reference`
were added to `supplyChainImpactFactKinds()`, those facts would arrive through
the uncharged base load; a new expansion loader would charge them.
The core test `TestShardScopedGenerationDoesNotLoadReferenceFacts` pins the
premise that the kind is not loaded, so a change to it forces this note to be
redone, and the vulnsource test
`TestCVEListShardGenerationsFactsAreRecordsPlusReferences` (R11) pins the
accounting rule. The reference count per shard was not measured in 5.1. T2 and
T3 are unaffected in kind: the 0.76-0.79 ms selection measurement ran the
`cve_list` severity path over 1,000 records, and the replay benchmark counts
dirty shards, not facts.

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
fails with `full-shard budget share = 0.010`, proving the check is not vacuous.
The share itself is a hypothetical bar, per the T1 restatement.

## Facts per shard

Facts are records times 1 plus distinct reference URLs (`R`). The rows below
are the record-only floor (`R` = 0) as measured in 5.1; add `R` facts to each
row for a real shard.

| Shard case | Records | Facts written (records + `R`) | Share of the 100,000 bar if charged (hypothetical) |
| --- | --- | --- | --- |
| Full upstream shard (`<NNNxxx>`, ≤1,000 records), floor | 1,000 | 1,000 + `R` | 0.010 at `R` = 0; under 0.10 while `R` < 9,000 |
| Observed partial shard, floor | 37 | 37 + `R` | 0.0004 at `R` = 0 |
| Full shard + 10x expansion evidence, floor | 1,000 + 10,000 | 1,000 + `R` written; 10,000 expansion envelopes are loaded, not written | 0.11 at `R` = 0 if expansion is charged (it is), still loads |

The `cve_list` envelope emits `vulnerability.cve` and `vulnerability.reference`
facts, so facts are not one to one with records. Collection-side
note: the workflow admits at most 100 CVE IDs per `cve_list` claim
(`maxVulnerabilityCVEListRequests`), so a full shard collects in at least 10
claims, each of at most 100 records plus their reference facts; the
reducer-side per-shard intent sees up to 1,000 + `R` facts.

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

This is a hypothetical bar, not a gate the shard meets: the production budget
does not charge a shard's own base load (see the T1 restatement), and reference
facts are not loaded by the reducer. As a mechanical check, charging a
record-only full shard (1,000 envelopes) against a real default
`newSupplyChainImpactEvidenceBudget(0)` keeps loading (`charge` returns true,
`exhausted()` false); the zero `supplyChainImpactTruncation` is neither
`partial()` (upsert-only, no spurious retraction freeze) nor `any()` (no
suppression-candidate discard). T4 is therefore not applicable to a shard's base
load: the hypothetical bar passes, and the live bound is the per-claim write
cap. The one row where the budget really charges is the "10x expansion
evidence" case, and that is expansion evidence, not the shard.

## Residual (not measured here)

No live Neo4j ran on this host, so no `EXPLAIN`/`PROFILE` of graph
projection at full-shard volume is attached (NOT_CHECKED). The change adds
no Cypher shape — no `MATCH`/`MERGE`/`UNWIND` was added or altered — and
projection for `cve_list` facts rides the existing source-agnostic writer
the `nvd`/`osv` facts already use, so graph cost scales with the fact volume
measured above (≤1,000 records per shard, ≤~12k per heavy day, plus the
reference facts each record carries, which were not counted in 5.1). Recording a
Neo4j-backed projection profile at full-shard volume is recommended as a
5.2 acceptance criterion, not a reason to redo 5.1.

Correction 2026-11-07: the "Neo4j-backed projection profile" recommendation
above is void — no Neo4j write path for these facts exists in-repo
(verified: zero vulnerability-projection Cypher; projection is reducer-side
Postgres). #10354 criterion 1 now measures the shipped Postgres projection
path (build wall-clock plus writer-transaction EXPLAIN) instead. The
in-memory numbers above stand unchanged.

## Filing verdict

- **5.2 (CVE List baseline + `deltaLog.json` tail): FILE.** The 5.1 theory
  is measured at the record-only floor, and the reducer-side cost measurements
  (T2, T3) stand. There is no reducer-side gate on a shard's base load, so no
  headroom figure applies to it; the live bound is the per-claim write cap, and
  a full shard can overflow it (see the T1 restatement). Gate the 5.2 issue on
  this note, carry the Neo4j projection profile above as a 5.2 acceptance
  criterion, and add "count reference facts per shard" and "measure claim-cap
  overflow for a full shard" to it.
- **5.3 (OSV bulk per ecosystem): DO NOT FILE.** It needs 5.2's
  cursor-continuity proof first, per the phase gate.
- **5.4 (catalog read surfaces): DO NOT FILE.** It waits on the owner's
  catalog scope decision (arbiter ruling DEFER text) in addition to 5.2.

No-Regression Evidence: the committed substrate is pure accounting plus
tests — `CVEListShardForCVEID`, `CVEListDirtyShards`,
`CVEListShardGenerations` in `internal/vulnsource` (dependency-light:
`strings`/`sort`/`strconv` only, no new import direction), and two
contract tests listed in the T1 restatement. No Cypher, graph write, queue, lease, worker, batching, or
concurrency change. Focused suites green:
`go test ./internal/vulnsource/ ./internal/reducer/supplychain/core/
./internal/workflow/` (exit 0), `go vet` on touched packages (exit 0),
`git diff --check` clean.

No-Observability-Change: no new metric, span, log key, status, or runtime
knob. The thresholds above are acceptance numbers in this note, not shipped
signals. Operators diagnose shard generations through the existing
source-snapshot and readiness surfaces.
