# #7930 scoped content-search work-bound guard and probes

## Binding ruling (arbiter, guard renegotiation accepted)

1. The bitmap-free guard is SUPERSEDED by a work-bound guard: 10 shipped
   cells (S1-S7 single-scope via searchFileContentPage, M1-M3 multi-scope via
   searchFileContentInRepos) x 2 regimes (custom default PREPARE/EXECUTE;
   generic force_generic_plan) = 20 cells, custom+generic mandatory.
   Per-cell caps on warm-cache EXPLAIN (ANALYZE, BUFFERS): shared-hit buffers
   <= 400 with reads = 0; Heap Blocks exact <= 100; rows removed <=
   max(200, 10x(limit+offset)). Only Actual Loops > 0 nodes count;
   never-executed branches are reported, not failed. No wall-time cap.
2. HOLD on MATERIALIZED-prefix and multicolumn-GIN (both KILLED with measured
   evidence). AUTHORIZED probes: (A) per-repo UNION ALL of single-scope legs
   for the multi builder (targets the ANY-defeats-GIN mechanism); (B) steer
   custom toward the BitmapAnd generic already finds, for rare/empty
   single-scope terms (predicate/statistics investigation only). Kill rule:
   any of the 20 cells regresses vs shipped work-bounds, the healthy
   common-term custom path regresses, or equivalence fails. REDIRECTED (not
   built): plan_cache_mode forcing, enable_* toggles, new indexes,
   selectivity-routed two-shape queries.
3. Equivalence oracle: DB-derived expected rows (never hand lists); matrix
   unicode, %_-escapes, offsets 0/10, limit+1 lookahead trim, zero limit,
   exhausted offset, empty/duplicate/multi grants; DB ORDER BY under
   en_US.UTF-8 is the ordering oracle; custom<->generic byte-identical
   ordered 8-field rows per cell with bound prepared params. Differential
   queries derive from the shipped builders with hermetic prefix guards;
   nothing is hand-copied.

## What changed (no serving change)

- `content_reader_search_page.go`: extracted `fileContentSingleScopeWhere`,
  `fileContentMultiScopeWhere`, `fileContentScopedQuery` (byte-identical SQL;
  pinned by `TestScopedSearchShippedDerivationGuard`). No predicate, order,
  limit, span, or argument change.
- New guard `content_reader_search_scoped_workbound_live_test.go` (20 cells),
  shared fixture `content_reader_search_scoped_fixture_test.go`, probes
  `content_reader_search_scoped_union_probe_live_test.go` (A + B with
  derivation guards). Tests skip cleanly without
  `ESHU_SCOPED_SEARCH_PROOF_DSN` + `ESHU_SCOPED_SEARCH_PROOF_DISPOSABLE=1`.

## Benchmark Evidence: work-bound guard (RED) and probes (measured)

Fixture: disposable PG, 12,000-file `scope-repo-big` + 11 x 300-file repos
(15,300 rows), shipped `content_files_content_trgm_idx` GIN. Shipped cells:

| Cell | Regime | hit | read | heap | removed (cap) | Verdict |
|---|---|---|---|---|---|---|
| S1 dense p0 | custom+generic | 12 | 0 | 0 | 30 (200) | GREEN (healthy baseline) |
| S2 dense p10 | custom+generic | 23 | 0 | 0 | 60 (200) | GREEN |
| S3 medium | custom+generic | 114 | 0 | 0 | 447 (200) | RED, residual (no authorized probe) |
| S4 rare | custom+generic | 18 | 0 | 3 | 0 (200) | GREEN |
| S5 empty, no trigram | custom+generic | 3606 | 0 | 0 | 15300 (200) | RED, Seq Scan full recheck |
| S6 unicode | custom+generic | 18 | 0 | 2 | 8 (200) | GREEN (BitmapAnd) |
| S7 underscore | custom+generic | 12 | 0 | 0 | 30 (200) | GREEN |
| M1 multi dense | custom+generic | 12-20 | 0 | 0 | 30 (200) | GREEN |
| M2 multi empty | custom+generic | 219 | 0 | 73 | 900 (200) | RED |
| M3 multi dup grants | custom+generic | 22 | 0 | 0 | 60 (200) | GREEN |

Custom and generic plans/rows are identical in all 20 cells (no
custom/generic divergence anywhere). Equivalence oracle
(`TestScopedSearchCustomGenericEquivalenceLive`): custom<->generic<->reader
byte-identical ordered 8-field rows for every cell plus the edge matrix
(upper-case pattern, literal `%`, limit+1 trim single+multi, zero limit,
exhausted offset, empty/duplicate/reordered grants). GREEN.

Probe (A) per-repo UNION ALL (legs byte-derived from the shipped builders,
grants deduped, ANY absent by guard): equivalent to shipped+reader in all
M-cells x both regimes (no KILL). M1 probe hit=270/heap=72/removed=0 and M3
probe hit=180/heap=48/removed=0 stay within caps but cost 8-22x the buffers
of shipped; M2 probe hit=225/heap=75/removed=900 is NO-GAIN (red where
shipped is red). Verdict: KILLED by no-benefit, not adopted; S1 healthy
custom path untouched.

Probe (B): the only still-red rare/empty single-scope cell is S5, where
custom and generic are the same Seq Scan and generic holds no BitmapAnd, so
there is nothing to steer toward (investigation only, no change). S3/M2 red
cells sit outside (B) scope and stay residual for a later rung.

## Observability Evidence: no serving change

No-Observability-Change: no table, index, queue, worker, route, metric,
span, or log change. The path stays diagnosed through the existing
`postgres.query` spans (`db.operation` search_file_content_page /
search_file_content_in_repos) and buffer/row evidence above.

## Disclosure and limits

Fixture PG 18.6 vs serving 18.3; synthetic corpus (no production rows);
single-sample cells (buffer counts were stable across runs, wall time was
not, and wall time is not capped at this rung); collation en_US.UTF-8/UTF8
asserted in-fixture. No serving claims: nothing here predicts production
latency. NOT_CHECKED (rung-3/4): built-binary replay, p50/p95, standby
replay, pooler characterization.

## Rung-3/4 serving proof (2026-10-15, QA, Eshu 8ee65f5, ledger 177)

Built-binary replay through the deployed serving path (MCP search_file_content,
same shapes as the guard cells, 1 cold + 10 warm each):

- S3-analog (single scope, medium term decode, limit 10): cold 0.326 s,
  p50 0.137 s, warm p95 0.157 s, 11/11 HTTP 200, single ordered digest, 6 rows.
- S5-analog (single scope, no-trigram term zz, limit 10): cold 0.127 s,
  p50 0.131 s, warm p95 0.173 s, 0 rows, digest stable.
- M2-analog (3 scopes, zz, limit 10): cold 0.116 s, p50 0.110 s,
  warm p95 0.122 s, 1 row, digest stable.

Pooler characterization: single-story pooler acquisitions sub-ms median
(see #8140: 21 reader spans, median start-gap 0.16 ms, one 81 ms wait).
Generic-plan regime stays covered by the guard's custom/generic EXPLAIN
rungs on fixture; no serving change alters the plan choice.

Reading: on the deployed 177 schema (stored lowercase + trigram GIN), the
S3/S5/M2 shapes serve in ~0.15 s with stable rows. The guard RED was plan
evidence on the pre-177 fixture; serving on the shipped schema does not
reproduce a tail. No probe ships: probes A/B stay killed (measured
no-gain), bitmap-free guard stays superseded, trigram-GIN preservation
stands. Guard + kill record pinned by the tests in this change.
