# #6834 — Code divergence theory proof

Gates epic #6833. No production code lands until the verdicts in §10 are
recorded. Status: measurements complete locally; remote-host timing and
full-corpus NornicDB PROFILE on indexed data remain pending (blocker §11).

All numbers below are labelled **verified** (observed by the author on the
machine in §1) or **reported** (from another source, quoted with provenance).
A theory resting on one sample is marked unproven — none below is.

## 1. Provenance (verified)

- Eshu worktree: `eshu-6834-code-divergence-theory` at `4008440b1` (origin/main
  at fetch time). Shim: untracked `go/scratch6834shim/main.go`, never committed,
  deleted before any evidence commit. GOCACHE `/Users/linuxdynasty/Library/Caches/go-build`
  (local disk), GOTMPDIR `/tmp/eshu6834`.
- Machine (local): MacBook Pro, darwin/arm64, go1.27.1. All timings are
  same-machine relative only: `absolute_target_applicable=false`. The reference
  profile is the remote validation host (r7a.4xlarge); those runs are pending §11.
- Revision note (v2): PR review found the v1 shim omitted production-emitted
  shapes (TS generators/arrows/variable-bound values, Python lambdas), used
  synthetic repo boundaries for SQL, asserted an unproven band-cap recall
  claim, and never bound the K-path LIMIT; the v1 arithmetic "8,450 pairs"
  was also wrong (8,385). All are re-measured below; the v1 numbers they
  replace are summarized next to their replacements, not quoted verbatim.
- Postgres scratch: `postgres:18-alpine`, image digest
  `sha256:6c538e7206ea40ff740ef27883529390a690b6ead6ba96b44c67a9f7c638e8fd`
  (matches repo compose pin `postgres:18-alpine`), throwaway container, scratch
  tables only.
- NornicDB: source `~/os-repos/NornicDB` at `b847df49`, binary built with
  `go build -tags 'noui nolocalllm'` (documented fallback; `make build-headless`
  needs a missing llama lib on this laptop), sha256
  `e96bc5b051ee2212cdbf9ff0cb7af09d9440cf06b136a4714321d41c328f4817`,
  `version` → `NornicDB v1.3.2`. Checkout restored clean afterwards.

## 2. Fingerprint shim design (verified)

Language-generic tree-sitter leaf walk over the function **body** node, using
the repo's own grammar bindings (`go/internal/parser/runtime.go` loaders).
Function nodes mirror production emission exactly (verified against
`javascript/language.go`, `python/language.go` + `lambda_support.go`,
`golang/language.go`, `java/parser.go`): Go `function_declaration`/
`method_declaration`; Python `function_definition` plus assignment-bound
`lambda` (named) and bare `lambda` (anonymous, skipped when it is an
assignment RHS to avoid double-count, exactly as production does); TS/TSX
`function_declaration`/`method_definition`/`function_expression` plus
`generator_function_declaration`, `generator_function`, `arrow_function`,
and `variable_declarator` with a function value (emits once under the bound
name, children not re-walked); Java `method_declaration`/
`constructor_declaration`. Body via `ChildByFieldName("body")` (arrow and
lambda bodies included). Whitespace never appears as leaves, so
whitespace-stripping is structural, not a normalization step. Capture proven
synthetically: generator + bound arrow + function expression (TS) and named +
assigned + anonymous lambdas (Python) all emit with correct names.
Corpus definition excludes the scratch shim dir itself (v1 accidentally
fingerprinted its own 16 functions; v2 deltas below are shape-driven, not
self-measurement: Go rows identical v1→v2 at 77,983).

Per-language tables were corrected empirically with a leaf-kind dump before
measuring (initial hypotheses were wrong in three places):

- Go string contents are `interpreted_string_literal_content` /
  `raw_string_literal_content` / `escape_sequence` leaves, not a single literal
  node; all three are now literal-class.
- Python strings are `string_content` (+ structural `string_start`/`string_end`)
  leaves; `string_content` is literal-class.
- TypeScript strings are `string_fragment` leaves; Java strings are
  `string_fragment` and Java integers are `decimal_integer_literal` (not
  `integer_literal`). Both corrected.
- Identifier rule is substring-based (`kind` contains `identifier`), which
  covers `type_identifier`, `field_identifier`, `package_identifier`,
  `property_identifier`.
- `.tsx` must use `LanguageTSX`: of the 288 `.tsx` files, 221 produced
  `has_error` trees under the plain TypeScript grammar (67 JSX-free files
  parsed clean even with the wrong grammar); with the TSX grammar only one
  file (`apps/console/src/pages/AdminPage.tsx`, 1 function still extracted)
  reports an error. `tests/.../syntax_error.py` is an intentional negative
  fixture: 0 functions extracted, correctly flagged.

Fields per function: `body_fp_exact` (sha256 over `kind+text` leaves, comments
excluded), `body_fp_renamed` (identifiers → positional `I<n>`, literals →
positional `L<n>`), `body_sketch` (128-register MinHash over 5-token shingles,
stdlib FNV+splitmix only), `body_token_count`, 32×4 LSH bands.
Determinism: two full scans byte-identical (`cmp_exit=0`).

## 3. Parse cost (verified, local laptop)

`bench` mode times parse vs walk+hash per file (single-threaded), v2 shapes:

| lang | files | funcs | parse total | walk+hash total | parse/file | walk/file | overhead |
| --- | --- | --- | --- | --- | --- | --- | --- |
| go | 14572 | 77983 | 6.22s | 21.97s | 0.43ms | 1.51ms | 3.5× |
| typescript | 537 | 5849 | 0.19s | 1.07s | 0.35ms | 1.99ms | 5.7× |
| tsx | 288 | 4657 | 0.14s | 1.07s | 0.49ms | 3.72ms | 7.6× |
| python | 67 | 223 | 0.004s | 0.008s | 0.06ms | 0.13ms | 2.1× |
| java | 32 | 83 | 0.001s | 0.003s | 0.05ms | 0.09ms | 1.9× |

The new shapes are mostly small (arrows, lambdas), so per-function walk cost
fell (TS 0.22→0.18ms, TSX 0.45→0.23ms) while per-file overhead rose — more
functions per file, each hashed. 0 parse-error files for Go/Java/TypeScript;
1 residual TSX (`AdminPage.tsx`, partial parse still yields its function) and
the intentional `syntax_error.py` fixture (0 functions, flagged). The walk is
naive recursion (`Child(i)`) that hashes every function regardless of floor,
so these ratios are an **upper bound**; a cursor walk plus hashing only above
the token floor will cost less. Absolute ingest impact must still be measured
on the remote host against the named baseline before #6835 merges.

## 4. Bucket histograms (verified, 88,795 functions, full local repo, v2 shapes)

Equality buckets at token floors 30/50/80/120 (multi = buckets with >1 member):

| floor | exact multi | renamed multi | exact top sizes | renamed top sizes |
| --- | --- | --- | --- | --- |
| 30 | 1227 | 4388 | 138,130,124,120,110 | 165,142,142,130,128 |
| 50 | 741 | 3252 | 130,124,120,110,89 | 165,130,128,120,119 |
| 80 | 334 | 2093 | 130,34,26,16,13 | 130,67,53,49,43 |
| 120 | 113 | 1153 | 130,13,9,6,5 | 130,38,28,20,19 |

Largest buckets hand-labelled (all verified by reading members):

- 130 identical `recordAPICall` (270 tokens) + 120 identical `isThrottleError`
  across `go/internal/collector/awscloud/services/*/awssdk/`: hand-maintained
  per-service wrappers, no `Code generated` marker. This is simultaneously the
  epic's best example and its biggest noise class: one family is
  130×129/2 = 8,385 pairs by itself. Decision: equality grouping reports them,
  the LSH path bounds them with the per-entity budget in §6 (not a naive row
  cap — see the recall measurement), and the read surface needs a named
  suppression rule with per-rule counts (§9), never silent filtering.
- Genuine small clones: `cloneStringMap` ×3 services, `securityGroupARN`
  (rds+redshift), `unionKinds` (Go+JS+Python dataflow), `mustEvalSymlinks` ×3
  test files, `parseArgs` across collector cmds, `ReadStatusSnapshot` fakes.
- Boilerplate that must suppress cleanly: table-driven test scaffolds
  (dominate renamed buckets at every floor), `testBoundary`, runtimebind
  `init`, fake SDK paginators.

Floor recommendation: default floor 50 (exact multi-buckets drop 1227→741,
all-remaining are actionable; floor 30 keeps test-scaffold noise). Floor stays
a tunable with counted suppressions.

## 5. Drift precision (verified, 500 hand-labelled pairs)

LSH candidates (32×4 bands over renamed 5-shingles, floor 30) on Go code
(Go shapes unchanged v1→v2, so the ledger stands as measured):
64,548 functions → 270,301 pairs with exact Jaccard ≥ 0.5. Evenly-spaced
deterministic sample, 100 per Jaccard band, bodies read for every ambiguous
case (ledger: 500 rows with pair ids, verdict, class).

| band | labelled | genuine | coincidence | ambiguous | precision |
| --- | --- | --- | --- | --- | --- |
| 0.9–1.0 | 100 | 99 | 1 | 0 | 99% |
| 0.8–0.9 | 100 | 100 | 0 | 0 | 100% |
| 0.7–0.8 | 100 | 98 | 2 | 0 | 98% |
| 0.6–0.7 | 100 | 95 | 5 | 0 | 95% |
| 0.5–0.6 | 100 | 94 | 5 | 1 | 94.9% |
| total | 500 | 486 | 13 | 1 | 97.4% |

Drifted (non-identical, shared-origin) exemplars confirmed: worker-count env
constant (`ESHU_PROJECTOR_WORKERS` vs `ESHU_PARSE_WORKERS`), first-nonempty
empty-case return (`""` vs `"node"`), servicekind test padding variants,
`mergeContractPayload` error-swallowing variant, `Scan` method drift across
services, same-file adapter siblings. Coincidence classes (each needs a named,
individually tested suppression rule in #6836): small-literal-list one-liners,
struct-fixture-builders of different types, field-projector loops,
test-idiom skeletons, bare predicate OR-chains, loop-skeleton converters.
Ambiguous case (clone-vs-index, pair 89) is kept as the fixture ambiguous case.

Threshold: ship `drifted` at Jaccard ≥ 0.7 (measured precision ≥98%);
0.5–0.7 stays available ranked, never finding-grade.

## 6. Grouping SQL (verified, postgres:18-alpine §1, v2 rows: 88,795 / 2.84M bands)

`EXPLAIN (ANALYZE, BUFFERS)` under the production boundary — one `repo_id`
for the whole corpus (v1 used synthetic per-directory repos; re-measured
after review, direction confirmed, magnitude held):

- Equality grouping, floor 50, no index: 31.0ms, seq scan + hash aggregate.
  **With** `(repo_id, fp_renamed)` index: planner still ignores it (full scan
  + hash aggregate is optimal at this scale). Verdict: ship #6836 with **no**
  grouping index; revisit at 10× corpus. Grouping path touches only narrow
  fingerprint columns, never `source_cache` (a #6835 contract gate).
- LSH band self-join, `LIMIT 1000`: 563.5ms unindexed (v1, hash join + temp)
  → 4.0ms single-repo with `(repo_id, band_no, band_hash)` (nested-loop index
  scans). The band index is **required** for #6837.
- Full self-join count (no LIMIT), single repo: 26,074,599 raw pairs in 9.5s
  (v1 pseudo-repos: 22,652,636 in 8.4s — cross-directory matches included, as
  predicted). Band skew: 87% of pairs come from band-rows with >50 members.
- Cap recall (measured after review — the naive row-cap-50 is **wrong**):
  of 297 genuine ship-band (Jaccard ≥ 0.7) ledger pairs, only 107 share a
  band-row ≤ 50 members; 190 share only mega-rows, and just 72 of those share
  an equality fingerprint — so cap-50 plus an equality backstop would still
  lose 118 genuine pairs. Refined rule: **per-entity candidate budget** —
  each entity verifies at most K partners, ordered by shared-band count desc.
  Measured rank needed over the 297 ship pairs: worst 189, p50 17, p99 171
  (292 directly ranked; 5 missed only by pair-orientation in the analysis
  join, symmetric so the same bound holds). **K = 200 preserves 100% of
  measured ship-band recall** with work bounded per entity regardless of
  mega-row size. This replaces the row-cap as the #6837 scaling bound;
  the reducer verifies Jaccard only within budget, partitioned by `repo_id`,
  and counts budget-exhausted entities in telemetry rather than silently
  dropping them.

## 7. NornicDB PROFILE (verified, binary §1, in-memory fixture: wrapper
fan-in 60 + 8 bypassers, 12-cohort, 9-edge chain diamond)

All plans anchor on `NodeIndexSeek (:Function)` — no unanchored scans:

| query | rows | db hits | total time |
| --- | --- | --- | --- |
| Q1 bypass one-hop (callers of T) | 9 | 511 | 287µs |
| Q1b wrapper fan-in (callers of W) | 1 | 412 | 205µs |
| Q1c wrapper callees (W→?) | 2 | 511 | 102µs |
| Q2 cohort majority-callee (12 members) | 1 | 2401 | 316µs |
| Q3 shortestPath depth ≤4 | 1 | 110 | 150µs |
| Q3k bounded multi-path `*1..4 LIMIT 5` | 4 | 421 | 99µs |

Correctness on fixture verified (Q1 returns W + 8 bypassers; Q2 finds guard
11/12 with the inferred edge visible; Q3k returns all 4 simple paths
a-b-c-d-e, a-b-e, a-x-e, a-y-e). LIMIT binding re-measured after review on a
7-path fixture: `LIMIT 5` returns exactly 5 rows with db hits flat at 421
(identical to the 4-path run) — expansion stops when the row cap fills, so
cost is bounded by the cap, not the available path count. Verdicts:
wrapper-bypass and cohort queries are one-hop/cohort-bounded by
construction — **go**. Bounded K-path enumeration with early `LIMIT` — **go**
for `compare_code_paths` with depth ≤ N and row cap in the query text
(re-profile on indexed data in #6838).

## 8. Finding contract (new, binds #6836–#6839)

Every finding returns `reasons[]` (one entry per contributing signal: edge,
bucket, score component) with `score == sum(reasons)` — covered by a unit test
that mutates each reason and asserts the sum. Truth level is always `derived`,
never `exact`; graph-finding confidence inherits the weakest contributing
CALLS edge; inferred edges are surfaced as such. Every response carries
per-rule suppression counts; no silent filtering. #6851 (general import
cycles) is independent of this epic — not taken unless explicitly asked.

## 9. Package ownership (verified)

Dirgate cap is 40 non-test Go files per directory; over-cap dirs are
grandfathered, so new code goes in new directories (grandfather rows:
`internal/parser` 47, `internal/query` 276, `internal/reducer` 123):

- Fingerprint leaf-walk: new `go/internal/parser/fingerprint` (parser owns
  language behavior per the ownership table).
- Read surfaces: `go/internal/query/codedivergence` (as #6836 already states).
- Reducer domain: new `go/internal/reducer/codedivergence` (reducer owns
  cross-domain materialization).
- Every new package ships `doc.go`, `README.md`, `AGENTS.md`; files under
  500 lines. OpenAPI, handler tests, `http-api.md`, and MCP reference move in
  lockstep. Capability-matrix rows claim local profiles only until
  `docs/internal/remote-validation/code-divergence.md` exists (#6840).

## 10. Verdicts per child

- #6835 (fingerprints): **go** — leaf-walk proven deterministic across Go,
  Python, TypeScript, TSX, and Java; error policy proven (skip file at
  0 functions, exclude `has_error` graphs). Minor contract change, additive
  columns only.
- #6836 (reads): **go** — grouping needs no index; suppression catalogue and
  the `reasons[]`/`score` invariant are specified in §5/§8.
- #6837 (LSH reducer): **go with bounds** — band index required,
  per-entity candidate budget K = 200 ordered by shared-band count desc
  (replaces the disproven row-cap-50; preserves 100% of measured ship-band
  recall), partition by `repo_id`, budget exhaustions counted in telemetry;
  worker/batch cuts are not an acceptable fix.
- #6838 (wrapper/cohort): **go** — one-hop/cohort-bounded shapes PROFILE
  anchored (§7).
- #6839 (`compare_code_paths`): **go conditionally** — bounded K-path proven
  (4 paths, 421 hits, early LIMIT); ship only with depth cap and row cap in
  the query text, re-PROFILEd on indexed data, else drop with reason recorded.
- #6840 (validation + docs): **go last** — remote runs in §11 plus
  `remote-validation/code-divergence.md`.

## 11. Pending (remote-gated, not theory risks)

- Remote-host timing for §3–§7 on the reference profile (host unreachable
  from this network: SSH to the validation host times out, no VPN interface;
  needs owner network path). Local numbers stand as relative, same-machine
  evidence only.
- Full-corpus NornicDB PROFILE on indexed data (replaces §7 fixture scale).
- 20-repo golden-corpus fingerprint run (local run covered this repo plus
  fixtures: 88,795 functions across Go, Python, TypeScript, TSX, and Java).

## 12. #6838 slice C ship proof (compare_code_paths, bounded BFS)

No-Regression Evidence (#6838): baseline is base 3fbf13ea6, where the
route, tool, and matrix row do not exist. After adds a read-only route
plus tool with no shared-code change to existing reads: the full
`./internal/query/...`, `./internal/mcp/...`, and
`./internal/capabilitycatalog/` suites pass (91 packages ok, 0 FAIL),
`capability-inventory verify` is clean, and the generated catalog is
regenerated idempotent (141 entries / 627 surfaces).

Benchmark Evidence (#6838): backend NornicDB
`timothyswt/nornicdb-cpu-bge:v1.2.3@sha256:4dfa887d990bf0b536693830830e34351c036716b0fe6dc957e1a3680e9f3c74`
(fixture diamond, same-container live gate
`-tags live_nornicdb_wrapper_bypass`):
`TestLiveNornicDBCompareCodePaths` 0.05s — 4 distinct simple paths,
depths 2..4 shortest-first, `truncated=false`, request caps K=5/N=4
against the 500-expansion visit budget;
`TestLiveNornicDBWrapperBypass` 0.02s. Input shape is the anchored
one-hop CALLS read per BFS expansion (no variable-length path
projection: live probes showed NornicDB returns empty path content,
so enumeration rides Go BFS with depth cap 6, path cap 20, and the
visit budget — the §10 bound condition met by construction rather
than by LIMIT text). Terminal row counts: 4 emitted paths, bounded
hop reads, `visited` rides the response as the operator budget signal.

Observability Evidence (#6838): no new handler span (sibling
call-chain parity); the traversal budget rides `visited`, the truth
envelope rides every response, and member hydration adds one traced
`postgres.query` (`divergence_members_by_entity`). Matrix row
`call_graph.compare_code_paths` claims local profiles only.

## 13. #6839 ship proof (convention_outlier, cohort sweep)

No-Regression Evidence (#6839): baseline is origin/main at 3c532d219,
where the convention_outlier kind does not exist. After adds a read-only
kind plus one codequery file with no shared-code change to existing reads:
the full `./internal/query/...`, `./internal/mcp/...`,
`./internal/queryplan/`, `./internal/capabilitycatalog/`, and
`./internal/ask/...` suites pass, `capability-inventory verify` is clean,
and no generated artifact changes (descriptions and kind enums ride no
generated file: the catalog tracks route/tool names only, unchanged here).

Benchmark Evidence (#6839): backend NornicDB
`timothyswt/nornicdb-cpu-bge:v1.2.3@sha256:4dfa887d990bf0b536693830830e34351c036716b0fe6dc957e1a3680e9f3c74`
(seeded fixture: 5-handler router cohort, 3-member interface cohort,
2 package cohorts, 9-member callee fan-out; same-container live gate
`-tags live_nornicdb_convention_outlier`):
`TestLiveNornicDBConventionOutlier` 0.57s with per-read client medians
(n=3) cohorts-interface 264us, cohorts-router 247us, cohorts-package
235us, callee-edges 232us. Input shapes are the three repo-wide
one-hop enumeration reads (Function anchor, repo/grant in the anchoring
WHERE) plus the 50-key-chunked UNWIND callee-edges read through the
already-pinned `runWrapperGraphRows` runner, so the queryplan bound
(50 keys x corpus CALLS degree 8, audit max_results 400) covers the new
reads with no new row. Same-engine dialect parity holds: the
Neo4j-rendered callee-edges read returns exactly the NornicDB-rendered
row set on the NornicDB container. Cross-backend parity holds: identical
four canonical findings on NornicDB and on Neo4j community 2026.05.0
(`ESHU_LIVE_GRAPH_BACKEND=nornicdb|neo4j`, same assertions both runs).
Full-corpus PROFILE stays remote-gated under #6840 like §11.

Dogfood precision (#6839): stdlib-AST extraction of name-based call sets
(builtins dropped as the graph emits no builtin CALLS edges, test files
excluded) over Eshu's own tree, package cohorts, shipped defaults
(min 3, share 0.6, cap 50). Headline cohort (28 `handle*` methods in
`go/internal/query/codequery`): 7 verdicts, including WriteSuccess at
19/28 with 9 outliers — all 9 verified by source read as true
non-direct-callers (factual precision 9/9, zero false call-set claims),
and all 9 reach WriteSuccess within two hops through response writers or
delegates (4 carry the one-hop mediated shape on resolved edges:
writeCallChainResponse plus the deadcode analyzer's Handle methods; the
code-flow family needs two hops, which mediation does not flag — a
documented depth limit). Broad sweep (400 dirs): 106 verdicts,
TrimSpace-dominated whole-package majorities; spot-checked secretcrypto
Errorf 6/9 (outliers EnvelopeKeyID/Open/fingerprint verified true
non-callers). Caveats: name-based identity collides (`common` x5,
`Validate` x2 noted), package scope only (no interface/router cohorts in
dogfood: Go structural implements is out of scope and HANDLES_ROUTE needs
an indexed graph), so this measures selection precision on real call
sets, not end-to-end recall.

Observability Evidence (#6839): no new handler span (sibling graph-track
parity); the cohort source, share, outliers, and mediation ride the
finding body, the truth envelope rides every response, and member
hydration reuses the traced `divergence_members_by_entity` read. No new
capability row: `code_divergence.findings` covers the fifth family on the
existing local profiles.
