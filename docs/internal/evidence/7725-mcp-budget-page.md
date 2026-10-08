# MCP Budget Page Evidence (#7725)

`find_code` and `search_entity_content` advertise `limit` maximum 200, but a
legal request at that maximum over a common name returned
`mcp_response_over_budget` (610,762 and 884,737 serialized bytes against the
262,144-byte budget in the 2026-10-08 read sweep). The error carried no rows and
no way to reach the remainder.

## Page size model

A throwaway test (not committed) built rows shaped like a common-name hit: a
12 KiB source body and a 2 KiB docstring, which the read-time clips cut to
4,096 and 512 bytes, then dispatched `limit` 200 with the budget disabled and
measured the single-copy (resource-only) size the guard compares to the budget.

| Tool | Rows | Single-copy bytes | Bytes per row | Rows that fit 262,144 |
| --- | --- | --- | --- | --- |
| `search_entity_content` | 200 | 1,038,916 | 5,194 | about 50 |
| `find_code` | 200 | 1,486,172 | 7,430 | about 35 |

### Production row sizes (read-only, ops-prod)

The arbiter required real sizes. The `search_entity_content` shape for pattern
`decode` with no repository (limit 200, the sweep's call index 1 and 9) was run
as a read-only SELECT on the PG reader (`default_transaction_read_only=on`,
`statement_timeout=25s`) with the row shaped by the route's clip rules
(`source_cache` 4,096, `docstring` 512, fingerprint keys stripped) and only
sizes returned. The 200 rows serialize to 447,180 bytes in the embedded
resource form (mean 2,228, max 6,342 bytes per row), against 262,144, so 133 of
200 rows fit. The sizes agree with the observed 884,737-byte two-copy reply
(about 404 KB structured plus 447 KB escaped resource plus envelope) within 4%.
`find_code` for query `decode`, language php, repository `r_957cd853` takes the
graph path, which Postgres cannot reproduce. Its page size is derived from the
observed 610,762-byte two-copy reply, not measured: about 1.6 KB per row, so
about 160 of 200 rows fit. The trim does not depend on either number; it
measures each candidate page.

Both calls returned the over-budget error before the change. The model is
therefore "whole rows by serialized size", not a fixed row count: the fit count
varies with the row, so the trim measures the real size.

## Change

- `trimToBudgetPage` (`go/internal/mcp/dispatch_budget.go`) runs after the
  resource-only fallback fails. For the two row-list tools it binary-searches the
  largest first run of whole rows whose resource-only envelope is within budget,
  using the same `estimateResponseBytes` measure the guard uses, so a returned
  page fits by construction. It sets `data.truncated`, `data.next_offset`,
  `data.budget_page`, a `truth.omissions` entry, and recounts the clip markers
  over the kept rows. The over-budget error stays for other tools and for a
  first row that is over budget alone.
- The trim sits at dispatch, not in the handlers: the budget is an MCP
  dispatch concern, and only the dispatcher can measure the final envelope
  (summary text, truth block, JSON escaping inside the embedded resource). A
  handler-side byte estimate would drift from that measure.
- `find_code` gains `offset`. The handler asks the store for
  `offset + limit + 1` rows and skips `offset`; the effective limit shrinks so
  `offset + limit` stays within the 200-row ranked window, which keeps the store
  probe at or below its existing 201-row cap. An offset of 200 or more is a 400.
  The response carries `offset` only when the request set a positive one.
- The repository-anchored graph query orders by `e.name, e.id`. `e.name` alone
  is not a total order, so tied rows could swap across the page edge between
  calls. The global content path already ends its sort on `entity_id`.

## Page order with the hybrid re-rank

The independent review (F1) found that a budget page lost and repeated rows when
the hybrid re-ranker was on. `next_offset` is a position the `offset` parameter
reads, but the trim cut the re-ranked order, and `find_code` re-ranked the whole
probe window, whose size changes from page to page.

Invariant: `offset` is a position in the lexical (offset) order, and the
hybrid re-rank only orders rows inside one page. A page is always the window
`[offset, offset + n)` of the offset order, so the union of the windows along a
walk is every row exactly once, whatever the limit or the re-rank did.

- `find_code` content fallback cuts the page from the offset order first
  (`codemodel.CodeSearchPageRows`), then re-ranks inside the page. A re-rank
  over `offset + limit + 1` rows changed with the window, so the order a client
  paged through changed per page; the in-page re-rank does not.
- `search_entity_content` already re-ranked inside the SQL page. Each row of a
  re-ranked page now carries `page_position`, its place in the offset order
  (set only when the re-rank changed the order, so other responses keep their
  bytes). The trim keeps the rows with the smallest `page_position` in their
  displayed order, and `next_offset = offset + keep` is then exact.
- A page whose `page_position` values are not a permutation of `0..n-1` is not
  cut: the dispatcher keeps the over-budget error envelope rather than guess.
- Alternatives rejected: (ii) re-rank one fixed 201-row window on every page
  would add an 8x store fetch for small limits and cannot apply to
  `search_entity_content`, whose offset reaches 10,000; (iii) disabling the
  re-rank when `offset > 0` or on a trim changes ranking that clients see.
- Behaviour change to note: with a ranker on and more than `limit` rows, the
  `find_code` content fallback page is the first `limit` rows of the lexical
  merge, re-ranked, where before it was the top `limit` of a re-rank over up to
  `2 * (limit + 1)` rows. A request whose rows all fit the page is unchanged.

Proof: `TestRerankedBudgetPagesLoseAndRepeatNoRow` walks both tools with rankers
that reverse a window and that move the last row to the front (the row at the
cut), at limits 200 and 25, by `next_offset` under a forced small budget and by
plain offset. It requires every id exactly once, each page to be a contiguous
window of the offset order, and an identical second walk. Before the change it
failed on both tools (for example `find_code` limit 25: page 1 held
`entity-025` and the reverse of the window).

## Tie-break cost evidence

Neo4j (ops-prod, 2026.08.1, read-only shim, `PROFILE`, same statement text apart
from `ORDER BY e.name` versus `ORDER BY e.name, e.id`):

| Repo (files / rows before Top) | Db hits old / new | Delta | Warm time |
| --- | --- | --- | --- |
| 758 files / 1,190 rows | 30,578 / 31,567 | +3.2% | equal (133 vs 131 ms cold, 55 vs 56 ms) |
| 12,402 files / 19,477 rows | 229,539 / 248,815 | +8.4% | +7% to +13%: profiled 158-164 ms became 176-179 ms; unprofiled warm 119 ms became 132 ms (about 13-17 ms) |

Both plans have the same operator tree and use `Top` (`Top name ASC LIMIT 201`
becomes `Top name ASC, entity_id ASC LIMIT 201`), with no `Sort` and no scan or
expand change. The one new cost is the `e.id` property read on every row before
`Top`. `LIMIT` stays 201 at limit 200: the 200-row window caps the fetch, so a
request at offset 100 sends the same statement as offset 0 and the larger-fetch
half of the theory does not exist. The result is marginal against the +10% time
bar on the largest repo and is accepted because without `e.id` the order is not
total and tied rows can swap across a page edge.

Limits of this evidence: the shim graph is not the sweep's state (the sweep's
`decode`/php repository has 0 matches there, so it proves nothing); no repo with
100k or more matching entities (cost is about one db hit per pre-`Top` row);
one client on a host that was not quiet (profiled n=3, warm n=2), so this is not
a timing proof; no NornicDB; no Go-path run against Neo4j; the live
`query-plan-regression` gate is deferred at pre-push and runs in the deeper local
preflight and in CI; its result is recorded in the PR (expected to pass because
the anchor `NodeUniqueIndexSeek` is unchanged).

The repository content fallback read `SearchEntitiesByName` ordered by
`relative_path, start_line` only. It now ends on `entity_id`, like
`SearchEntityContent` and the candidate queries. A throwaway PostgreSQL 18.6
table (400,000 rows, the `repo_id`, `relative_path` and trigram indexes, about
10,000 rows matching the repo and the name) gave the same plan before and after
(`Incremental Sort` on the presorted `relative_path`, one `Index Scan` on
`content_entities_path_idx`), the same 1,082 buffers at limit 26 and 8,125 at
limit 201. `SearchEntitiesByLanguageAndType` still orders by
`relative_path, start_line, entity_name`; `find_code` does not read it, so it is
left as is.

The trim cost: `BenchmarkTrimToBudgetPage` (200 rows of about 5 KiB, Apple M5
Max, `-benchtime=2s -count=3`) takes 3.1 to 3.4 ms, 9.2 MB and 2,740 allocations
per trim on a quiet-ish host (an earlier busier run gave 4.2 ms and 9.8 MB). It
runs only on a response that returned an error before this change.

Before figure, same 200-row input: the path the old dispatcher took for it
(measure both copies, measure the resource-only copy, build the over-budget
error) costs 3.8 to 4.1 ms, 13.0 to 14.8 MB and 3,662 allocations per call,
measured with a scratch benchmark that calls `estimateResponseBytes` twice and
`overBudgetResult` (those functions are unchanged by this change). The trim is
not slower than the error path it replaces, and it returns rows instead of an
error.

No-Regression Evidence: a request that fits the budget is unchanged, with two
opt-in exceptions, both only when the hybrid ranker is on: (1) the `find_code`
repository content fallback with more than `limit` rows pages by lexical order
(see the behaviour change above); (2) every row of a page the re-rank reordered,
in either tool and including a page with at most `limit` rows, gains
`page_position` (`content_handler_rerank.go:43-44`, `codequery/handler.go:226-234`).
The ranker is off in the deployment where this was seen: a read-only `kubectl`
read on 2026-10-08 of the live `eshu-api` and `eshu-mcp-server` container
environments found no `SEMANTIC` or `EMBED` variable and no `envFrom`. The test
`TestBudgetPageLeavesFittingResponsesUnchanged` compares the rendered result
with the guard on and off and requires equal bytes, no `budget_page`,
`next_offset`, or `truth.omissions`, and no `truncated` or `omissions` key in the
rendered truth (removing `omitempty` from `TruthEnvelope.Truncated` fails it).

Observability Evidence: `eshu_dp_mcp_response_budget_page_total{tool}` counts
pages. The `mcp tool response budget page` log carries `tool`, `response_bytes`,
`emitted_bytes`, `budget_bytes`, `rows_returned`, and `rows_available`. The
existing `eshu_dp_mcp_response_bytes` histogram still records the attempted size.
