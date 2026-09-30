# #7385 Expose The prior_failure A Work Item Kept

#7320 keeps a superseded work item's old failure under
`failure_details.prior_failure`. #7388 (merged, `d2e9e601f`) extends that to a stale-scope reclaim
and an operator note, and #7407 (merged, `eea68a679`) caps newly stored details text at 4,096 bytes. Nothing read the kept
failure back. This change exposes it, additively, by reading the keys wherever a
writer put them.

## Change

- `GET /api/v0/freshness/generations`: `latest_failure` gains `prior_failure`
  (`status`, `failure_class`, `failure_message`, `updated_at`). The newest-failure
  LATERAL selects `failure_details` and `scanGenerationLifecycleRow` parses it in
  Go: a JSON object with an object-valued `prior_failure` fills the field, anything
  else (free text, an array, a bad value, empty) gives nil and no error. No
  `IS JSON` in SQL, so one non-JSON row cannot fail the page. `updated_at` is
  normalised to RFC3339 UTC like `observed_at`. The prior failure's own
  `failure_details` text is not decoded and is not on the wire.
- `POST /api/v0/admin/work-items/query` (and the dead-letter and skip
  responses; a replay clears `failure_details`, so it carries neither; `operator_note` is populated by the #7388 operator-note writers, merged): the list query and the shared RETURNING lists select
  `failure_details`; `WorkItem.PriorFailure` and the previously unused
  `WorkItem.OperatorNote` are filled from it and rendered. This route has no
  OpenAPI response schema, so it is documented in `status-admin.md`.
- CLI `eshu freshness generations` prints ` prior_failure=<class>` after
  ` failure=<class>`, only when the kept failure has a class. The MCP tool `get_generation_lifecycle` changes its
  description only; there is no input or output schema change, and the surface
  inventory check passes.
- The OpenAPI schema for `latest_failure` gains an optional `prior_failure`
  object; `scripts/verify-openapi.sh` passes.

## Proof

| Test | Asserts |
| --- | --- |
| `TestListGenerationLifecycleParsesPriorFailure` | the fold's JSON fills status, class, message and a normalised `updated_at`; the marshalled `latest_failure` has no `failure_details` |
| `TestListGenerationLifecyclePriorFailureIsNilWhenDetailsCarryNone` | free text, array, object without the key, a non-object value and empty all give nil with no error |
| `TestListGenerationLifecycleQuerySelectsFailureDetails` | the read selects `work.failure_details` and has no `IS JSON` |
| `TestFreshnessGenerationLifecycleExposesPriorFailure` | the HTTP handler returns `latest_failure.prior_failure.failure_class`, with no details text |
| `TestRunGenerationsRendersPriorFailure` | the CLI line contains ` failure=<class> prior_failure=<class>` and prints nothing extra without one, or when the kept failure has no class |
| `TestApplyWorkItemDetailsParsesNoteAndPriorFailure`, `...LeavesFieldsNilWithoutAnObject`, `...DecodesTheKeysIndependently` | the admin listing parser, including free text and null, and that a wrong-typed `operator_note` or `prior_failure` does not discard the other key, in both directions |
| `TestListWorkItemsQuerySelectsFailureDetails` | the list query and the mutating RETURNING carry `failure_details` |
| `TestAdminWorkItemListingExposesPriorFailureAndNote` (live, real schema) | a superseded row and a noted row return their prior failure and note; free-text and NULL rows return neither |

## Performance

Performance Evidence (#7385): the change adds one column, `failure_details`, to the
existing newest-failure LATERAL of the lifecycle read. The plan does not change (the
same `ORDER BY updated_at DESC ... LIMIT 1` per generation); the cost is reading and
sending the text. Postgres 18 (`postgres:18-alpine`), the shipped
`listGenerationLifecycleQuery` with and without the column (before derived from the
shipped constant), one page of 500 generations (the maximum), each with 3 failure
rows carrying folded details, 15 alternated runs, min/median/max, host load high so
the ratio is what matters:

| Details width | Before | After |
| --- | --- | --- |
| about 800 B | 4.92 / 5.39 / 7.40 ms | 5.48 / 6.13 / 7.45 ms (+0.74 ms median) |
| about 4 KB (the cap #7407 set) | 4.39 / 4.75 / 5.96 ms | 6.80 / 7.40 / 10.65 ms (+2.65 ms median) |

That is +14% and +56% on the median at the maximum page, worst case for the
column: every generation returns a superseded row with folded details. A typical
page is smaller and most failure rows carry short details. The read is an operator
drilldown, not a claim or projection path. NOT_CHECKED: a page against a large
production table, and the admin listing's added column. Rows written before #7407
merged can hold longer details.

Observability Evidence: no new signal. The read's existing bounded-read
telemetry and the `page.truncated` flag are unchanged; a row whose details cannot
be parsed shows no `prior_failure`, the same as a row that has none.
