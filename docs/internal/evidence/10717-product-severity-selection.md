# #10717: product-path severity selection cost

Refs #10717. Part of #9209. The product path now reads severity from the
best-scored observation of a CVE group instead of the one row source rank
picks (`severityObservation` in `go/internal/reducer/supplychain/core/product.go`).
This note records the cost of that selection and why it is safe.

**Decision, threshold, deadline.** Decision: is one linear pass over a CVE
group's observations cheap enough to run per CVE group? Threshold, stated
before the final measurement: under 10 microseconds per CVE for groups of up to
8 observations, and no allocation when no row needs its vector parsed. Real
group sizes were not measured; 8 is a generous guess, and 64 is reported as a
stress size only. Deadline: one measurement session.

## Measurement

Benchmark: `BenchmarkProductSeverityObservation`, a hermetic in-memory Go
benchmark (no database, graph, or queue), Go 1.27.3 on `darwin/arm64` (Apple M5
Max). Two input shapes, each over two sources:

- `score-last`: only the last row carries a score and no row has a vector, so
  every observation is read but none is parsed.
- `vector-only`: every other row carries a CVSS 3.1 vector and no reported
  score, so each read also parses a vector, because `cvss.DeriveSeverity` parses
  one only when the reported score is zero. This is the case #9693 made
  first-class, and it was missing from the first draft of this note.

Command: `go test ./internal/reducer/supplychain/core/ -run '^$' -bench
BenchmarkProductSeverityObservation -benchtime=200000x -count=3`. Figures vary by
about 25% between sessions on this machine (an earlier session read 70 ns, 410
ns, and 2.9 microseconds for `score-last`), so read them as indicative of one
session.

| Shape, observations | Baseline (before: read the ranked row only) | After, ns per CVE (3 runs) | Allocations |
| --- | --- | --- | --- |
| score-last, 1 | no loop, constant | 85.3, 81.3, 83.4 | 0 |
| score-last, 8 | no loop, constant | 473.6, 457.4, 481.1 | 0 |
| score-last, 64 | no loop, constant | 3,389, 3,496, 3,554 | 0 |
| vector-only, 1 | no loop, constant | 89.6, 93.5, 96.1 | 0 |
| vector-only, 8 | no loop, constant | 6,637, 6,309, 5,334 | 3,712 B, 24 allocs |
| vector-only, 64 | no loop, constant | 41,910, 44,618, 41,869 | 29,696 B, 192 allocs |

Result against the threshold: `score-last` is linear at about 53 ns per
observation with zero allocations. `vector-only` stays under 10 microseconds up to
8 observations but allocates, because each vector parse allocates; at 64
observations it costs about 42 microseconds and 192 allocations. The no-allocation
part of the threshold holds only when no row needs its vector parsed. The pass
runs once per CVE group (`severityObservation` is called outside the per-product
loop), after the index is built, on rows already loaded in memory. It adds no
query, queue item, lease, retry, or batch, so terminal queue and row counts are
unchanged (no queue or row is written by this change).

## Why this is safe

- Finding identity is unchanged: advisory id, dates, and finding id still come
  from the rank-chosen representative (`supplyChainImpactFindingID`).
- The change picks among observations the reducer already loaded; it performs
  no new I/O.
- The evidence list gains one fact id when a different observation supplied the
  score, and the list is already deduplicated in `finalize`.
- One projected behavior changes on purpose: a rejected or withdrawn record lends
  no severity on the product path. Before, a rejected ranked row with no
  withdrawal timestamp still reported its own score; now a live sibling replaces
  it even when that sibling is unscored, so a rejected scored representative with
  an unscored live sibling reports score 0 (for example 9.9 becomes 0). A group
  with no live observation keeps the representative and its own score.

No-Regression Evidence: the benchmark above shows 80 ns to 3.6 microseconds per
CVE with zero allocations when no vector is parsed, and 5 to 7 microseconds with
24 allocations for an 8-row vector-only group (about 42 microseconds and 192
allocations at 64 rows, a stress size). No Cypher, graph write, queue, lease, worker,
batching, or concurrency change; focused suites
(`go test ./internal/reducer/supplychain/...`) and `go vet` pass.

No-Observability-Change: no metric, span, log key, or status was added. An
operator sees the effect in the finding's score, vector, label, and
`evidence_fact_ids` through the existing supply-chain impact finding read
surfaces and the reducer's existing handler spans.
