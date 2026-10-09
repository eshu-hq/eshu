# Evidence: provenance count tiebreaker ordering (#7711)

The `ProvenanceCountStore` reads (`EdgesBySourceTool` per verb,
`FilesByLanguage`) ordered grouped counts by `cnt DESC` alone. Tied
groups then deliver in backend-undefined order, which flips the
order-sensitive backend-diff digest (`DigestRows` with `ordered=true`)
while the counted multiset agrees — the corpus quorum reproduces
`row digest differs` at 24/24, 18/18, and 126/126 rows on the
`DEPENDS_ON`, `DEPLOYS_FROM`, and `File`-language reads.

The fix adds the group key as an `ORDER BY` tiebreaker (`cnt DESC,
source_tool` / `cnt DESC, language`). The consumer
(`provenanceCountMap`) folds rows into a `map[string]int64`, so delivery
order is irrelevant to the product: the tiebreaker only stabilizes the
wire order, never the counts.

No-Regression Evidence: this is a pure correctness fix on the
provenance gauge scrape path (periodic telemetry reads, not the write
hot path). Old vs new statement text timed on the same 30-group tied
seed, 50 runs each, pinned backends: NornicDB
(`ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-500-e022384c`) old mean
175.953µs vs new mean 191.751µs; Neo4j (`neo4j:2026-community`) old
mean 1.502859ms vs new mean 1.519104ms. No measurable regression; the
added sort key orders at most the group cap (10,000) in-memory rows,
126 observed in the corpus. Query shape unchanged otherwise:
relationship-type-anchored aggregates and the File-label-anchored
group, same indexes, no constraint change.

No-Observability-Change: the gauge series names, labels, and scrape
path are untouched; only the row delivery order of the underlying
reads changed. The backend-diff quorum going quiet on these three
statements is the observable effect, verified by the next main
differential run, not by a new signal.

Row-level proof: a seeded dual-backend run (tied groups, per-backend
shuffled write order) showed `multiset_equal=true digest_equal=false`
on the old text and `digest_equal=true` on the tiebreaker text for all
three reads. The committed `provenance_counts_tiebreak_live_test.go`
(class ci, both backends) asserts the exact tiebreaker row sequence on
each leg; it fails on the old text (`bravo/3` delivered before
`alpha/3`) and passes on the new text.
