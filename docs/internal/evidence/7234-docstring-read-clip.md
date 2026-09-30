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
map, since the store may hand every caller the same one) and a graph row's
top-level `docstring` to `DocstringClipBytes` = 512, UTF-8 safe, and marks the
row with `docstring_clipped`, `docstring_clip_bytes`, and
`docstring_total_bytes`. On the three routes whose rows carry fields derived from the metadata
(`find_symbol`, `inspect_code_inventory`, `find_code`) it then calls
`entitysemantics.ReattachSemanticSummary` for each clipped row, so
`semantic_summary`, `semantic_profile`, the language blocks, and `story` are
rebuilt from the clipped value and no echo exceeds 512 bytes. The fourth route,
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
1.5 KB other row content + six 512-byte echoes) is about 170 KB, under the
262,144-byte dispatch budget with one wire copy.

## Before / after (default-args reply, MCP dispatch)

Fixture: 40 entities per store, each with a 16,413-byte docstring
(`TestDocstringToolsDefaultResponseStaysWithinBudget`), reply measured by
`estimateResponseBytes`.

| Tool (default rows) | Before | After |
| --- | --- | --- |
| `find_symbol` (20) | `mcp_response_over_budget`, 3,341,129 B | 164,043 B |
| `inspect_code_inventory` (20) | `mcp_response_over_budget`, 3,347,569 B | 170,483 B |
| `search_entity_content` (10) | 174,021 B (no markers) | 31,045 B |

Before the fix the first two tools fail closed at default arguments for a
repository with long docstrings; after it they return a page whose size no
longer depends on docstring length.

## Not covered

- Other `metadata` fields are not clipped. A row whose metadata is large for
  another reason can still exceed the budget and fails closed as before.
- No production or ops-qa numbers: docstring length distribution on the
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
