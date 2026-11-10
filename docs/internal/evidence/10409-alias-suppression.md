# Alias-Aware Suppression Matching Evidence (#10409)

Issue #10409 (item 3.4) completes alias-aware VEX/suppression matching over
advisory-group member sets in the supply-chain impact reducer pass:

- each populated scope identity field (`cve_id`, `advisory_id`) is tested
  against the finding's full matchable set — group member ids plus the
  finding's own `cve_id` and `advisory_id` — conjunctively, case-folded as
  before (ruling D1). `upstream` and `related` ids never enter the set, and
  ungrouped/quarantined fallback findings keep the exact {cve_id,
  advisory_id} set.
- the persisted decision records the matched finding-side id, the scope
  field that supplied it, and whether the match was direct or through a
  group member (`matched_id` / `matched_field` / `matched_via` on the
  suppression block).
- suppression-generation diff rows carry advisory ids on both sides, so an
  advisory-scoped change reopens its holders through the existing three-arm
  holder lookup (ruling D4). The unreachable-scope counter now counts only
  suppressions naming neither a CVE nor an advisory id and is renamed from
  `eshu_dp_supply_chain_impact_suppression_no_cve_scope_total` to
  `eshu_dp_supply_chain_impact_suppression_no_identity_scope_total`.
- matching runs through a per-pass candidate index keyed by case-folded
  anchor values (ruling D2) instead of an O(findings × suppressions) scan.
  The index is a pure prefilter: evaluation rechecks adjacency and scope on
  every candidate, non-ASCII or anchorless scopes ride a full-scan
  fallback, and `TestSuppressionIndexMatchesFullScan` proves indexed and
  full-scan decisions identical over a seeded mixed corpus.

Benchmark Evidence: `go test ./internal/reducer/supplychain/core/ -run '^$'
-bench BenchmarkApplySupplyChainImpactSuppressions_SpreadIdentities` on
go1.26.6 linux/arm64 (200 grouped findings, 2,000 CVE-scoped suppressions at
the retained-candidate cap naming group-member ids, 20 iterations): baseline
full scan 44,668,624 ns/op (26,012 B/op, 801 allocs/op) on the pre-index
branch commit; after the index 1,303,657 ns/op (1,338,124 B/op, 5,818
allocs/op) — a 34x wall-time reduction for the whole suppression-evaluation
stage of one pass on the spread-identity shape. The pre-existing
single-finding benches are unchanged in shape and cost
(`BenchmarkEvaluateSupplyChainSuppression_AtCandidateCap` ≈ 430–525 µs/op
before and after, run-to-run variance on a shared host): the win comes from
not rescanning non-adjacent suppressions per finding, not from changing
per-candidate evaluation. The diff-statement widening adds two projected
`payload #>> '{scope,advisory_id}'` columns to the existing scoped
generation-pair read; it changes no predicate, join, or index, so the #10330
statement-cost proof for that read (143 ms p50 at 100,000 suppressions)
stands. Terminal queue and row counts are unchanged: no queue, lease,
schema, or graph-write behavior moves.

Observability Evidence: new counter
`eshu_dp_supply_chain_impact_suppression_identity_matched_total` (labels
`domain`, `matched_field` in cve_id/advisory_id, `matched_via` in
direct/group_member) emitted from
`SupplyChainImpactHandler.emitCounters`; the renamed no-identity-scope
counter, span attribute `no_identity_scope_rows`, log key, and
`suppression_diff_no_identity_scope` result sub-signal keep the #10019
unreachable-scope signal under its corrected name. The existing
`evaluateSuppressions` timing sub-signal covers the indexed stage. The
golden snapshot pins `matched_id`/`matched_field`/`matched_via` on the
CVE-2026-00010 expired-suppression finding in both the MCP and HTTP impact
finding shapes.
