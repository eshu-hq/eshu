# Docstring Read Clip Evidence (#7234)

#7171 clipped each row's `source_cache` to 4 KiB at read time, but a row's
docstring stayed whole and the row shape repeats it. A probe over the row
builders (`symbolEntityResults` plus `AttachSemanticSummary`) with a 2,000-byte
docstring found it in five fields on a TypeScript or JavaScript row
(`metadata`, `semantic_summary`, `semantic_profile`, `javascript_semantics`,
`story`) and six on a Python row (`python_semantics` as well): a 10.7-13.0 KB
row for a 2 KB docstring. The issue's "about five copies" is confirmed.

## Change

`querycontract.ClipRowsDocstring` cuts `metadata.docstring` (on a copy of the
map, since the store may hand every caller the same one) to
`DocstringClipBytes` = 512, UTF-8 safe, and marks the
row with `docstring_clipped`, `docstring_clip_bytes`, and
`docstring_total_bytes`. On the three routes whose rows carry fields derived from the metadata
(`find_symbol`, `inspect_code_inventory`, `find_code`) it then calls
`entitysemantics.ReattachSemanticSummary` for each clipped row, so
`semantic_summary`, `semantic_profile`, the language blocks, and `story` are
rebuilt from the clipped value, so every echo is derived from the 512-byte string (a quoted echo can run a few bytes over where quotes and newlines are escaped). The fourth route,
`search_entity_content`, builds rows from `EntityContentSearchRow` and derives
nothing, so the metadata clip alone bounds it. Responses
gain `docstring_clip_bytes` and `docstring_clipped_rows`, beside the
`source_cache` markers. The clip runs after the page is trimmed, where the
`source_cache` clip runs. Nothing is stored differently; `get_entity_content`
returns the whole docstring.

Why 512 bytes and a clip rather than dropping the echoes: dropping
`semantic_profile.docstring` or the language-block copies removes fields a
client may read, which is not additive. A clip keeps every field and bounds it.
The number is chosen against the budget: 20 rows x (4,096 source_cache + about
1.5 KB other row content + six 512-byte echoes) is about 175 KB in the worst
case, under the 262,144-byte dispatch budget as one wire copy (see the note
under the table on the single-copy regime).

## Before / after (default-args reply, MCP dispatch)

Fixture: 40 entities per store, each with a 16,413-byte docstring and a 12 KiB
`source_cache` (clipped to 4,096 by #7171), reply measured by
`estimateResponseBytes` (`TestDocstringToolsDefaultResponseStaysWithinBudget`).
The "before" column is the same test file run on `origin/main` (725fa1942).

| Tool (default rows) | Before | After |
| --- | --- | --- |
| `find_symbol` (20) | `mcp_response_over_budget`, 3,488,291 B | 156,995 B |
| `inspect_code_inventory` (20) | `mcp_response_over_budget`, 3,494,731 B | 160,399 B |
| `search_entity_content` (10) | 210,842 B (fits, no docstring markers) | 104,627 B |

Before the fix the first two tools fail closed at default arguments for a
repository with long docstrings; after it they return a page whose size no
longer depends on docstring length.

**Which copy the "after" figure counts.** The MCP result carries the envelope
twice, a `structuredContent` copy and an escaped `resource.text` copy. Two full
copies of a 157 KB reply are about 314 KB, over the 262,144-byte budget, so
`applyResponseBudget` drops the structured copy and keeps the single resource
copy when that fits; the figures above are that single copy. The reply is a
success, not `mcp_response_over_budget`, and the resource copy holds the whole
envelope, but a client that reads only `structuredContent` gets none on a page
this size. A page that fits both copies needs the rows to total about 131 KB, or
roughly 6.5 KB per row at 20 rows: 4,096 of source and about 1.5 KB of other row
content leave under 1 KB for all docstring echoes, which is below what a
docstring clip that keeps its summary line can offer. The 512-byte ceiling
targets the single-copy regime with margin (157-160 KB against 262 KB), not
the two-copy one. A fixture with a 512-byte `source_cache` understates a real
page, because the source body then contributes far less than the 4,096-byte
clip; this table uses 12 KiB bodies.

## Not covered

- Other `metadata` fields are not clipped. A row whose metadata is large for
  another reason can still exceed the budget and fails closed as before.
- No production or QA numbers: docstring length distribution on the
  reference corpus was not measured (local fixture only). The 512 ceiling is a
  size decision, not a measured percentile.
- No golden or cassette data carries the touched response fields
  (`rg source_cache_clip_bytes testdata` is empty), so no snapshot changed.

## No-Regression Evidence

No-Regression Evidence (#7234): the clip is per-page, in-memory work after the
page is fetched and trimmed. `BenchmarkClipRowsDocstring` (20 rows, every row
copying its metadata map and cutting a 16 KiB docstring, the worst case):
11.4-11.5 us and 7.2 KB, 80 allocs per page, three runs. A clipped row also pays
one `ReattachSemanticSummary`: `BenchmarkReattachSemanticSummary` 3.3 us,
4.1 KB, 29 allocs per row, so at most about 70 us per 20-row page. A row whose
docstring fits pays a type assertion and a length check. No SQL, Cypher, index,
transaction, queue, or lease path changes; the reads behind each page are
untouched.

## Observability Evidence

No-Observability-Change: no metric, span, or log is added. The clip is visible
to a caller through the row and response markers, and the existing
`eshu_dp_mcp_response_over_budget_total` counter and `mcp_response_over_budget`
error envelope still report any page that exceeds the budget for another reason.

## Sibling routes: dead code and complexity

#7468 left a follow-up: measure the other graph-backed tools that attach the
same docstring-derived echoes, and clip only the ones over the budget.
Fixture: 16 KiB docstring on every row, default arguments, reply measured by
`estimateResponseBytes` through the MCP dispatcher
(`TestDocstringSiblingToolsDefaultResponseStaysWithinBudget`,
`TestInvestigateDeadCodeFullSuppressedBucketStaysWithinBudget`,
`TestCyclomaticComplexityStaysWithinBudgetOverALongDocstring`). "Before" is the
same tests run without the clip; "After" is with it. Sizes are verified figures
from those runs; the budget is 262,144 bytes.

| Tool (rows) | Before | After | Share of budget |
| --- | --- | --- | --- |
| `find_dead_code` (25) | `mcp_response_over_budget` | 203,410 B | 78% |
| `investigate_dead_code` (25 active rows) | `mcp_response_over_budget` | 249,562 B | 95.2% |
| `investigate_dead_code` (25 active + 25 suppressed) | not measured | 222,078 B | 84.7% |
| `find_most_complex_functions` (10) | `mcp_response_over_budget` | 68,876 B | 26% |
| `calculate_cyclomatic_complexity` (one row) | 166,420 B, two copies | unchanged | 63.5% |
| `find_cross_repo_dead_code` (default limit, suppressed bucket included) | `mcp_response_over_budget`, 9,198,502 B | 235,095 B (single-copy fallback) | 89.7% |

Change: `find_dead_code`, `investigate_dead_code` (all three buckets,
`clipDeadCodeInvestigationDocstrings`), `find_cross_repo_dead_code` (active and
suppressed rows, `clipCrossRepoDeadCodeDocstrings`), and the list branch of
`find_most_complex_functions` call `querycontract.ClipRowsDocstring` with
`entitysemantics.ReattachSemanticSummary` after the page trim, then
`AddDocstringClipMarkers`. The fail-closed empty-grant complexity answer carries
the same markers, so a scoped caller with no grants cannot tell it from an
empty index by their absence
(`TestComplexityEmptyGrantAnswerHasTheShapeOfARealEmptyAnswer`).
`ReattachSemanticSummary` drops `derivedSemanticKeys` before it re-derives, so
that list must equal what `AttachSemanticSummary` writes;
`TestDerivedSemanticKeysMatchWhatAttachSemanticSummaryWrites` now pins it.

Not clipped, with the reason:

- `calculate_cyclomatic_complexity` with an entity id returns one row. At 16 KiB
  it is 63.5% of the budget; measured at 24 KiB it is 248,340 B (94.7%, two
  copies) and at 32 KiB 165,282 B via the resource-only fallback. Overflow is
  expected only past roughly 50 KiB per docstring, which is an extrapolation
  from those points, not a measurement.
- `inspect_call_graph_metrics` and `investigate_import_dependencies` were
  neither measured nor changed in this work; measuring them is left for a
  separate change.

Residual headroom: `investigate_dead_code` with 25 active rows sits at 95.2% of
the budget as two copies. That is thin. A row whose other metadata is large for
a reason other than the docstring can push it over. Note that the 25 active +
25 suppressed reply (222,078 B) is smaller than the 25-row one because it went
through the dispatcher's resource-only fallback, which drops the structured
copy: a client that reads only `structuredContent` gets none on a page that
size. `find_cross_repo_dead_code` is in the same position: 235,095 B (89.7%) at
its default limit, reachable only as the single resource copy. When even the
single copy does not fit, the reply fails closed with
`mcp_response_over_budget` as before. Other tools that echo a docstring
and were not measured here: `trace_call_chain` (relationship identity rows),
the shared `codequery` response helper, and the search enrichment path.

No-Regression Evidence (#7234): the sibling clip is the same per-page,
in-memory step as above and adds no SQL, Cypher, index, transaction, queue, or
lease work; the graph reads behind each route are untouched. Cost per row is the
benchmarked figure above (`BenchmarkClipRowsDocstring`, `BenchmarkReattachSemanticSummary`),
which was reported from the #7468 runs and not re-run for these routes. The
largest page is the dead-code limit of 500 rows plus at most 50 suppressed rows;
that scales the per-row figure to roughly 2 ms, an extrapolation and not a
measurement.

No-Observability-Change: no metric, span, or log is added. The clip is visible
through the row and response markers, and `eshu_dp_mcp_response_over_budget_total`
with the `mcp_response_over_budget` envelope still reports any page that
exceeds the budget.
