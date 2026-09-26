# Find Parallel Implementations With Code Divergence

Eshu's code-divergence report finds places where a repository grew more than
one way of doing the same thing, so a maintainer can knock them down to one.
Every finding is deterministic and reproducible: no LLM judges duplicates.
Every finding is labeled `derived`, never `exact` — each one is an inference
a maintainer must confirm.

## The five finding kinds

| Kind | What it means | Detected from |
| --- | --- | --- |
| `parallel_implementation.exact` | Two or more functions with the identical token stream (comments and whitespace removed) in different files. | Equal `body_fp_exact` fingerprints |
| `parallel_implementation.renamed` | Identical structure after identifiers and literals are alpha-renamed: the same body with different names. | Equal `body_fp_renamed` fingerprints |
| `parallel_implementation.drifted` | Near-duplicate pairs whose token-shingle sketches were similar enough to nominate and whose exact Jaccard similarity cleared the threshold: the copy that drifted since it was cloned. | Reducer-verified Jaccard on LSH-nominated pairs, with `similarity` reported |
| `parallel_implementation.wrapper_bypass` | Most callers reach target `T` through a thin, high-fan-in wrapper `W`; a minority call `T` directly and skip whatever `W` guarantees (auth, retries, telemetry). | One-hop CALLS fan-in around a qualified wrapper |
| `parallel_implementation.convention_outlier` | Of a same-role cohort (implementers of one interface, handlers on one router, same-role siblings in one package), the majority calls helper `H`; the reported members do not. | Cohort majority vote over bounded CALLS rows |

The graph kinds carry weakest-edge `confidence`: an `inferred` CALLS edge
can never produce a high-confidence bypass or outlier claim.

The fingerprints (`body_fp_exact`, `body_fp_renamed`, `body_sketch`,
`body_shingles`, `body_token_count`) are store-internal. They live in the
entity metadata the collector writes and in the `code_function_fingerprint`
tables the report reads, and are removed from every API and MCP entity row so
they do not consume the MCP response budget. Read a finding's `fingerprint`
field from the report, not from an entity's `metadata`.

## Start with the rollup

One call returns counts by kind plus the top findings per kind, through every
surface:

```bash
eshu analyze divergence --repo my-service --report --top 3
```

```json
// POST /api/v0/code/divergence/report
{"repo_id": "my-service", "top_per_kind": 3}
```

MCP clients call `report_code_divergence` with `repo_id` (required),
`top_per_kind` (default 3, max 10), and `include_tests` (default false).
The answer packet carries the standard `{data, truth, error}` envelope with
`truth.level: derived`. Operators watching the pipeline behind the report
start at [Code Divergence Signals](../reference/telemetry/code-divergence.md).

The response shape:

- `counts`: assembled post-suppression findings per kind, keyed by qualified
  kind name. All five keys are always present, even at zero.
- `total`: the sum across kinds.
- `top`: the highest-scoring findings per kind in final score order.
- `truncated`: per-kind flags. Content families scan every nominated group up
  to a 500-group hydration cap; `wrapper_bypass` qualifies families up to a
  200-family graph cap; `convention_outlier` always runs the whole cohort
  sweep. A capped window marks its kind `true` so a quiet count is
  distinguishable from a complete one.
- `suppressions`: per-rule counts (see below).
- `source_backend`: `postgres_content_store`, plus `+graph` when a graph
  finding rode the page.

Then page the full list per kind and drill into one finding:

```bash
eshu analyze divergence --repo my-service --kind drifted --limit 25
eshu analyze divergence --repo my-service --report --include-tests
```

`POST /api/v0/code/divergence/findings` pages with `limit`/`offset` in
deterministic score-desc, finding-id order. `POST
/api/v0/code/divergence/investigate` drills into one finding by `repo_id`,
`kind`, and `fingerprint` (for `wrapper_bypass` the fingerprint carries the
target entity id; for `convention_outlier` it carries the cohort address plus
the majority callee). MCP clients use `find_code_divergence` and
`investigate_code_divergence`.

## Read the evidence

Each finding carries:

- `members`: one entry per function with file path, start/end line, owning
  package, token count, and a `source_handle` pointing at `get_file_lines`
  and `get_entity_context` follow-ups.
- `reasons`: named signals whose values sum exactly to `score`. There are no
  hidden terms: value-bearing reasons decompose the score, zero-weight signal
  reasons (for example a large-body note) name judgment signals that carry no
  weight.
- `confidence` (graph kinds only): the weakest contributing CALLS-edge
  confidence.
- `cohort`, `majority_callee`, `share`, `outliers` (`convention_outlier`
  only): the cohort that voted, the helper the majority calls, the majority
  share, and the outlier member ids.

A finding with two members in different packages and a high member-tokens
product outranks a three-copy utility: nominations rank by members × tokens,
then finding id, and pages emit in final post-suppression score order.

## Tune the noise

Functions below 50 tokens never join a finding: below that floor, buckets are
scaffold noise, not actionable clones. Generated, vendored, and test files are
excluded by default, as are trivial accessors and intentional parallel
families (same-name wrappers repeated across five or more packages, such as
per-service API clients). Each exclusion is a named rule, and every response
counts suppressions per rule — a quiet result is distinguishable from a
filtered one:

- Content families: `below_floor`, `generated_file`, `vendored_path`,
  `test_file`, `trivial_accessor`, `wrapper_family`
- Wrapper bypass: `not_wrapper_family` (an exact group that is not a wrapper
  family), `wrapper_not_qualified` (no target behind the family qualified),
  `wrapper_graph_unavailable` (the graph backend was down, so the family
  counted what it could and said so instead of failing the call)
- Convention outlier: `below_min_cohort`, `outlier_unqualified`,
  `cohort_truncated`, `outlier_graph_unavailable`
- Timeouts (report only): `wrapper_graph_timeout`, `outlier_graph_timeout`.
  A graph track the bounded read budget cuts short degrades to a counted
  timeout with its kind truncated instead of failing the other four kinds.
  The findings pages keep the loud deadline so a slow track stays visible
  as a defect rather than a quiet zero
- Drifted pairs: `no_shingles` (rows from before shingle persistence),
  `equality_duplicate` (already reported as an exact finding),
  `similarity_below_threshold`, `candidate_budget_exhausted`

Opt test-file copies back in with `--include-tests` (or `include_tests`):
it covers the exact, renamed, and convention-outlier families. Drifted pairs
touching test files are dropped at write time, so the flag has no effect on
drifted findings — there is no test member to resurrect.

## When the report disagrees with the pages

`counts` are assembled post-suppression findings over the scanned window,
not raw group nominations: a family that suppresses away contributes its
suppression counts but no count. The findings pages remain the source of
post-suppression truth per kind; the report is the starting point, not the
audit trail.
