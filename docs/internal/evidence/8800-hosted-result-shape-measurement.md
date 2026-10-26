# Hosted MCP result-shape measurement (#8800)

The 2026-10-26 hosted run measured actual client input usage for `dual` and
`structured` MCP tool results against exact-one-copy prompt controls. For the
tested configuration, Claude Code's three-size structured/control slope ratio
was **0.999903**, within the #8800 limit of 1.10. Both clients answered the
four requested facts correctly under both shapes. Codex's token-slope result
is **not proven**: its input usage was nonmonotonic in two complete matrices.
The process default remains `dual`. This follows the earlier
[local result-contract proof](8800-structured-result-contract.md).

## Run identity and method

| Field | Captured value |
|---|---|
| Hosted MCP source build | `61cfc94fd84ee3397ce0b48d1ec6ed0a5da5ea57` |
| MCP protocol reported by `initialize` | `2024-11-05` in every captured run |
| Measurement window | Decision fixed 2026-10-26 21:28:29 UTC; model calls ended 21:39:33 UTC |
| Claude Code | CLI 2.1.310; actual model `claude-sonnet-5-5` |
| Codex | CLI 0.164.0; actual model `gpt-6.1-sol`, low reasoning effort |
| Route and concurrency | Router `call_tool`, one worker, read-only `list_indexed_repositories` |
| Result shapes | `dual`: text and embedded resource plus `structuredContent`; `structured`: one text plus `structuredContent` |
| Capture override | `MAX_MCP_OUTPUT_TOKENS=200000` for both MCP shapes |

Each client/shape pair requested the same deterministic final pages with
`limit` 1, 12, and 24. Offsets were fixed before the captures from a prior
calibrated list count using `max(total-limit, 0)`; the harness did not recompute
them for each call. The response at each size reported
`count=limit`, `truncated=false`, fresh truth, and `error=null`. The measured
`structuredContent` fragments were 1,153, 7,862, and 17,284 exact wire bytes.
Within each first-matrix size, both clients and both shapes received the same
fragment bytes; their SHA-256 values were respectively
`cecd5a0161c373cdd82e065598771a80406e6f6282c6f1e83f54aa6e6280395a`,
`aec23b8bb283094c1300497aa658fae98a105a4cbd5abbb637448f952774d2a9`,
and `cd8999e03cf512eccd2e139a3bc253bfdc7fc088f60c0c93351ab2dbf8ff8d80`.
The relay matched the router target and JSON-RPC response ID; it observed one
completed domain call per capture. All 12 first-matrix MCP captures and six
second-matrix Codex captures had valid wire shape, client usage, final answer,
and process exit 0.

Raw client events, exact wire bodies, one-copy prompts, usage, and the audit
ledger are retained privately for rechecking. The public note records only
the bounded measurements and non-sensitive wire hashes.

The no-tool controls put that same first-matrix fragment **once** into a fixed
prompt, alongside a wrapper-only baseline. Their four captures per client
were valid and exited 0. Prompt-fragment hashes matched the MCP fragments;
the controls invoked no MCP or built-in tool. For Claude, total input is
reported input plus cache creation and cache read tokens, each counted once.
For Codex, reported `input_tokens` already includes `cached_input_tokens`;
cached tokens were not subtracted. The wrapper-only totals were 2,845 Claude
and 13,619 Codex input tokens.

For each three-size series, ordinary least squares fits total client input
tokens `y` against exact fragment byte length `x`:

`slope = Σ(xᵢ − x̄)(yᵢ − ȳ) / Σ(xᵢ − x̄)²`.

The slope divides the structured MCP series by its exact-one-copy control
series to test #8800's size-dependent input-rate criterion. Fixed router and
tool overhead remains in the fitted intercept; the ratio does not compare
absolute prompt-token totals. The preparation plan proposed matched repeat
counts and paired ordering, but the actual run captured one repetition per
matrix cell. This is a bounded point estimate without a
stability or variance claim; the unfulfilled repeat plan is not an additional
#8800 acceptance condition.

## First complete matrix

| Client and shape | 1,153 B input | 7,862 B input | 17,284 B input | Slope, tokens/B | Slope / control |
|---|---:|---:|---:|---:|---:|
| Claude one-copy control | 3,304 | 5,977 | 10,175 | 0.427222 | 1.000000 |
| Claude structured MCP | 14,488 | 17,167 | 21,359 | 0.427181 | **0.999903** |
| Claude dual MCP | 14,966 | 20,278 | 28,708 | 0.854678 | 2.000548 |
| Codex one-copy control | 13,888 | 15,495 | 18,102 | 0.262239 | 1.000000 |
| Codex structured MCP | 84,675 | 113,874 | 61,228 | -1.721748 | **Not proven** |
| Codex dual MCP | 71,038 | 74,309 | 107,371 | 2.333901 | Not proven |

Claude's structured slope was 0.4271805348 tokens/B versus
0.4272218501 tokens/B for the exact-one-copy control, giving
0.999903292966573. Its dual slope was 0.8546778206 tokens/B, about twice
the control slope. This supports the one-copy rate for this Claude
configuration and these pages; it does not establish a general token saving
for other models, calls, pages, or settings.

## Codex second complete matrix

The first Codex structured series fell from 113,874 to 61,228 input tokens
as the payload grew. A predeclared second and final matrix requested direct
`call_tool` to remove the `find_tool` step. It still had one completed domain
call and correct answers in each cell, but its usage was nonmonotonic:

| Codex direct-call shape | 1,153 B input | 7,862 B input | 17,284 B input |
|---|---:|---:|---:|
| structured | 41,726 | 29,280 | 31,878 |
| dual | 42,030 | 45,319 | 36,485 |

The second matrix's medium and large fragments kept their byte lengths but
their selection timestamps changed. Their exact bytes matched within each
dual/structured pair, but the first-matrix controls were no longer exact
one-copy controls for those samples. The variable cached input totals are an
observation, not an established cause. No third matrix or model call was run.
The Codex token-slope criterion remains unproved.

## Answer scoring and remaining acceptance

For all captured MCP cells, each final answer correctly reported the four
requested fields: returned count, overall total, truncation flag, and truth
freshness. The scoring was limited to those fields. Claude's large-page answer
also mentioned missing repository-group evidence; that statement and its
last-ten stub references matched the captured fragment. The run did not score
all repository attributes, graph truth, or arbitrary client questions.

The remaining #8800 work is a valid Codex three-size comparison to an exact
one-copy control, manual checks on the other documented clients, and one
hosted week without an increase in over-budget responses. A final change to
the process default also needs the applicable golden-corpus and review gates
on its final head. The `MAX_MCP_OUTPUT_TOKENS` override means this run does
not prove behavior under clients' ordinary output cap. No runtime-latency or
backend-throughput change was measured. The separate #8799 counter window is
outside this result.
