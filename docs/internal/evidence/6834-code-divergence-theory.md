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
Function nodes: Go `function_declaration`/`method_declaration`; Python
`function_definition`; TS/TSX `function_declaration`/`method_definition`/
`function_expression`; Java `method_declaration`/`constructor_declaration`.
Body via `ChildByFieldName("body")`. Whitespace never appears as leaves, so
whitespace-stripping is structural, not a normalization step.

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
- `.tsx` must use `LanguageTSX`: parsing all 221 `.tsx` files with the plain
  TypeScript grammar produced `has_error` trees; with the TSX grammar only one
  file (`apps/console/src/pages/AdminPage.tsx`, 1 function still extracted)
  reports an error. `tests/.../syntax_error.py` is an intentional negative
  fixture: 0 functions extracted, correctly flagged.

Fields per function: `body_fp_exact` (sha256 over `kind+text` leaves, comments
excluded), `body_fp_renamed` (identifiers → positional `I<n>`, literals →
positional `L<n>`), `body_sketch` (128-register MinHash over 5-token shingles,
stdlib FNV+splitmix only), `body_token_count`, 32×4 LSH bands.
Determinism: two full scans byte-identical (`cmp_exit=0`).

## 3. Parse cost (verified, local laptop)

`bench` mode times parse vs walk+hash per file (single-threaded):

| lang | files | funcs | parse total | walk+hash total | parse/file | walk/file | overhead |
| --- | --- | --- | --- | --- | --- | --- | --- |
| go | 14573 | 77999 | 6.37s | 22.21s | 0.44ms | 1.52ms | 3.5× |
| typescript | 537 | 2173 | 0.19s | 0.47s | 0.36ms | 0.88ms | 2.5× |
| tsx | 288 | 1053 | 0.14s | 0.47s | 0.49ms | 1.64ms | 3.3× |
| python | 67 | 211 | 0.004s | 0.008s | 0.06ms | 0.12ms | 2.0× |
| java | 32 | 83 | 0.002s | 0.003s | 0.05ms | 0.09ms | 1.9× |

0 parse-error files for Go/Java/TypeScript. The walk is naive recursion
(`Child(i)`), so 2–3.5× is an **upper bound**; a cursor walk plus hashing only
above the token floor will cost less. Absolute ingest impact must still be
measured on the remote host against the named baseline before #6835 merges.

## 4. Bucket histograms (verified, 81,519 functions, full local repo)

Equality buckets at token floors 30/50/80/120 (multi = buckets with >1 member):

| floor | exact multi | renamed multi | exact top sizes | renamed top sizes |
| --- | --- | --- | --- | --- |
| 30 | 1176 | 4255 | 138,130,124,120,110 | 165,142,142,130,128 |
| 50 | 724 | 3189 | 130,124,120,110,89 | 165,130,128,120,119 |
| 80 | 329 | 2076 | 130,34,26,16,13 | 130,67,53,49,43 |
| 120 | 112 | 1149 | 130,13,9,6,5 | 130,38,28,20,19 |

Largest buckets hand-labelled (all verified by reading members):

- 130 identical `recordAPICall` (270 tokens) + 120 identical `isThrottleError`
  across `go/internal/collector/awscloud/services/*/awssdk/`: hand-maintained
  per-service wrappers, no `Code generated` marker. This is simultaneously the
  epic's best example and its biggest noise class: one family is 8,450 pairs
  by itself. Decision: equality grouping reports them, but the LSH path must
  band-cap them away (§7), and the read surface needs a named suppression rule
  with per-rule counts (§9), never silent filtering.
- Genuine small clones: `cloneStringMap` ×3 services, `securityGroupARN`
  (rds+redshift), `unionKinds` (Go+JS+Python dataflow), `mustEvalSymlinks` ×3
  test files, `parseArgs` across collector cmds, `ReadStatusSnapshot` fakes.
- Boilerplate that must suppress cleanly: table-driven test scaffolds
  (dominate renamed buckets at every floor), `testBoundary`, runtimebind
  `init`, fake SDK paginators.

Floor recommendation: default floor 50 (exact multi-buckets drop 1176→724,
all-remaining are actionable; floor 30 keeps test-scaffold noise). Floor stays
a tunable with counted suppressions.

## 5. Drift precision (verified, 500 hand-labelled pairs)

LSH candidates (32×4 bands over renamed 5-shingles, floor 30) on Go code:
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

## 6. Grouping SQL (verified, postgres:18-alpine §1, 81,519 rows / 26MB)

`EXPLAIN (ANALYZE, BUFFERS)`, pseudo-repo = first path segment (10 repos):

- Equality grouping, floor 50, no index: 20.1ms, seq scan + hash aggregate,
  3,275 buffers. **With** `(repo_id, fp_renamed)` index (5.8MB): 18.0ms —
  planner correctly ignores the index (full scan + hash aggregate is optimal
  at this scale). Verdict: ship #6836 with **no** grouping index; revisit at
  10× corpus. Grouping path touches only narrow fingerprint columns, never
  `source_cache` (a #6835 contract gate).
- LSH band self-join (2.6M rows / 370MB), `LIMIT 1000`: 563.5ms unindexed
  (hash join + 42k temp blocks) → 3.9ms with `(repo_id, band_no, band_hash)`
  (94MB, nested-loop index scans). The band index is **required** for #6837.
- Full self-join count (no LIMIT): 22,652,636 raw pairs in 8.4s, 47M buffer
  hits. Band skew: 88% of pairs come from band-rows with >50 members, 76%
  from rows >100 (one 478-member row = 114k pairs). Decision: band-cap 50
  (skip/count-suppressed rows above it — those families are already caught by
  equality grouping, verified §4). This is the #6837 scaling bound; the
  reducer verifies Jaccard only below the cap, partitioned by `repo_id`.

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
a-b-c-d-e, a-b-e, a-x-e, a-y-e). Verdicts: wrapper-bypass and cohort queries
are one-hop/cohort-bounded by construction — **go**. Bounded K-path
enumeration works with early `LIMIT` — **go** for `compare_code_paths` with
depth ≤ N and row cap in the query text (re-profile on indexed data in #6838).

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

- #6835 (fingerprints): **go** — leaf-walk proven deterministic across all
  four languages; error policy proven (skip file at 0 functions, exclude
  `has_error` graphs). Minor contract change, additive columns only.
- #6836 (reads): **go** — grouping needs no index; suppression catalogue and
  the `reasons[]`/`score` invariant are specified in §5/§8.
- #6837 (LSH reducer): **go with bounds** — band index required, band-cap 50,
  partition by `repo_id`; worker/batch cuts are not an acceptable fix.
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
  fixtures: 81,519 functions across all four languages).
