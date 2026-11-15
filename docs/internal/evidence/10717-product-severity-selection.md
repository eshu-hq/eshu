# #10717: product-path severity selection cost

Refs #10717. Part of #9209. The product path now reads severity from the
best-scored observation of a CVE group instead of the one row source rank
picks (`severityObservation` in `go/internal/reducer/supplychain/core/product.go`).
This note records the cost of that selection and why it is safe.

**Decision, threshold, deadline.** Decision: is one linear pass over a CVE
group's observations cheap enough to run per product finding? Threshold: well
under 10 microseconds per CVE at 64 observations (a group size chosen as a
generous upper bound; real group sizes were not measured), with no allocation. Deadline: one measurement session.

## Measurement

Benchmark: `BenchmarkProductSeverityObservation`, a hermetic in-memory Go
benchmark (no database, graph, or queue), Go 1.27.3 on `darwin/arm64` (Apple M5 Max). Input shape: a group of `n` observations from
two sources where only the last carries a score, so every observation is read
(the worst case). Command:
`go test ./internal/reducer/supplychain/core/ -run '^$' -bench BenchmarkProductSeverityObservation -benchtime=2000000x -count=3`.

| Observations in the group | Baseline (before: read the ranked row only) | After, ns per CVE (3 runs) | Allocations |
| --- | --- | --- | --- |
| 1 | no loop, constant | 69.8, 69.3, 70.5 | 0 B, 0 allocs |
| 8 | no loop, constant | 406.7, 423.2, 419.8 | 0 B, 0 allocs |
| 64 | no loop, constant | 2,958, 2,938, 3,008 | 0 B, 0 allocs |

Cost is linear in the group size at about 46 ns per observation. The pass runs
once per product finding, after the index is built, on rows already loaded in
memory. It adds no query, queue item, lease, retry, or batch, so terminal queue
and row counts are unchanged (no queue or row is written by this change).

## Why this is safe

- Finding identity is unchanged: advisory id, dates, and finding id still come
  from the rank-chosen representative (`supplyChainImpactFindingID`).
- The change picks among observations the reducer already loaded; it performs
  no new I/O.
- The evidence list gains one fact id when a different observation supplied the
  score, and the list is already deduplicated in `finalize`.

No-Regression Evidence: the benchmark above shows 70 ns to 3 microseconds per
CVE with zero allocations. No Cypher, graph write, queue, lease, worker,
batching, or concurrency change; focused suites
(`go test ./internal/reducer/supplychain/...`) and `go vet` pass.

No-Observability-Change: no metric, span, log key, or status was added. An
operator sees the effect in the finding's score, vector, label, and
`evidence_fact_ids` through the existing supply-chain impact finding read
surfaces and the reducer's existing handler spans.
