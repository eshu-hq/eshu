# Evidence: backend-diff reproduced-set dispositions (#7711)

Local `-phase=backend-diff` quorum runs against the CI
`differential-capture` artifact (run 37858175117) with the refreshed
allowlist surface exactly 6 reproduced divergences (the issue's 5 plus
the `WorkloadInstance` read, newly reproduced in both pairings):

1. `DEPENDS_ON` source_tool counts (24/24) — delivery order only.
2. `DEPLOYS_FROM` source_tool counts (18/18) — delivery order only.
3. `File` language counts (126/126) — delivery order only.
4. `WorkloadInstance` read (2/4 rows) — write-side, data-dependent.
5. quality-inspect read (1/1, `line_count` differs) — NornicDB bug.
6. `PackageVersion` count (1/0 rows) — write-side, data-dependent.

## Fixes in this PR

- Statements 1–3: group-key `ORDER BY` tiebreakers in
  `go/internal/storage/cypher/provenance_counts.go`. Seeded
  dual-backend proof (tied groups, per-backend shuffled write order):
  old text `multiset_equal=true digest_equal=false` on all three
  reads; tiebreaker text `digest_equal=true` on all three. Committed
  `provenance_counts_tiebreak_live_test.go` (class ci, both backends)
  asserts the exact tiebreaker row sequence per leg; RED on the old
  text (`bravo/3` before `alpha/3`), GREEN on the new text.
- Statement 5: pre-alias `line_count` operands in
  `go/internal/query/codequery/quality/inspect.go`. The pinned
  NornicDB drops a function-call subtrahend in
  `coalesce(a) - coalesce(b)` inside `WITH`-over-traversal (30 - 0
  instead of 30 - 1; isolated through bis-a…bis-m probes, including
  literal-only `coalesce - coalesce` agreeing at 29 on both).
  Aliased `end_line - start_line + 1` answers 30 on both backends;
  the exact production statement verified live on both. Upstream
  NornicDB executor bug filed separately; the builder comment cites
  this issue.
- Allowlist entry 34 refreshed to the post-#7744 `S2` statement text
  (`<-[r:HAS_DEPLOYMENT_EVIDENCE]-(source)` instead of `-[r]-(m)`).
  The old text matched no divergence (stale) and masked the quorum;
  the refreshed entry excuses again, proven by the local quorum run
  reporting the 6 instead of failing stale.

No-Regression Evidence: the tiebreaker adds one in-memory sort key
over at most the group cap (126 groups observed in the corpus):
NornicDB old mean 175.953µs vs new 191.751µs, Neo4j old mean
1.502859ms vs new 1.519104ms (same 30-group seed, 50 runs each, pinned
images) — no measurable regression. The inspect change replaces one
`WITH` expression list with two alias-only `WITH` stages over the same
anchored traversal (no new scan, same cardinality); the returned
columns and filter/order clauses are unchanged.

No-Observability-Change: gauge series, scrape paths, and the inspect
response shape are untouched. The observable effect is the advisory
differential going quiet on statements 1–3 and 5 on the next main run.

## Remainder (follow-up)

Statements 4 and 6 agree on minimal identically-seeded fixtures
(2/2 and 1/1 with equal rows), so the corpus 2-vs-4 and 1-vs-0 gaps
are write-side: the legs materialized different instance/version sets.
That needs corpus-scale row capture (recordings store digests, not
rows) or write-path investigation, tracked in the follow-up issue;
this PR is `Refs #7711`, not `Fixes`.
