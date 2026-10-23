# Advertise ten measured tools on core_router (#8759)

Status: Accepted for the QA-sample list below. The guardrails and the
measurement procedure were accepted by the arbiter ruling on #8759
(2026-10-22). The list was filled on 2026-10-23 from a shorter, sparser window
than the procedure asks for, so #8759 stays open for a 14-day refresh. This
record does not approve a default-surface change (#8760).

**`core_router` advertises the 10 tools with the largest share of measured MCP
calls on a QA deployment, beside `find_tool` and `call_tool`.** The list is in
`coreToolNames` in `go/internal/mcp/router/core.go`. It came from call counts,
not from docs or opinion.

`List: QA sample, 6 days of samples between 17 and 23 October 2026, not the 14 days the procedure asks for`

## Context

Under the `router` surface every new kind of question costs one more call.
Advertising about ten of the most-used tools directly removes that cost for
most traffic. Epic #8752 requires the list to come from measured per-tool call
counts, not from opinion, because a guessed list also contaminates the #8760
before and after comparison.

Phase 1 could not read the counts: the dev box role got a 403 on the
monitoring proxy of the QA cluster, and the pod's own `/metrics` endpoint timed
out. The orchestrator later read the series through the QA metrics datasource
and handed over the shares below. Facts, each with its source:

- Series: `eshu_dp_mcp_response_bytes_count`, label `tool`. The datasource
  returns every series twice with different label sets, and only one variant
  carries `pod`. Their 14-day totals agreed within about 2.5 percent (close, not
  identical), so one variant was used to avoid double counting: the selector
  `{pod!=""}`. Aggregation: `increase` over 14 days.
- Samples exist on only 6 days between 17 and 23 October 2026, so the effective
  window is about 7 days and the data is sparse. The series lists 54 tools, 25
  of them with samples. Absolute volumes stay out of this record on purpose.
- The QA deployment may not match real use. Its sample may be test-driver
  heavy: `list_codeowners_ownership` is the top tool at 29.6 percent, which a
  production-class deployment may not show. The epic's 163-tool figure came
  from a production-profile deployment.
- Retention: NOT_CHECKED.
- Over-budget check (procedure step 5, same variant, same window): three tools
  had over-budget responses, `get_service_story`, `list_indexed_repositories`,
  and `list_repository_files`. Only `list_repository_files` is in the core
  list, at about 6 percent of its own calls, which rests on a very small count.
  That is a reason to recheck it on the refresh, not a measured reason to drop
  it.
- The 10 chosen tools together carry about 84 percent of the sampled calls
  (the rounded shares in the table sum to 84.4). The other 29 tools have no
  samples in this window, which is not proof of zero use.

## Options

| Option | Cost | Verdict |
|---|---|---|
| Pick ten tools from docs, intents, or playbooks | Cheap | Rejected. It is the opinion the issue rules out and it contaminates #8760. |
| Keep the list empty until a 14-day production-class sample exists | `core_router` gives no round-trip saving, and #8760 cannot compare it with `router` | Rejected. A measured QA list is better than none, and the refresh stays open. |
| Fill the list from the QA sample now, top 10 by share | A list that may be test-driver heavy | Chosen. It follows the procedure; the caveats below and the open refresh bound the risk. |

## Decision

- `coreToolNames` holds the 10 tools in the table below, in share order.
  `TestCoreRouterToolSetMatchesMeasuredList` pins `core.go` to
  `measuredCoreToolNames` in `go/internal/mcp/core_tool_set_test.go`, so a list
  change needs a matching change to this table and to that slice.
- The list stays within three limits, enforced by
  `TestCoreRouterToolSetGuardrails`: every name is in `ReadOnlyTools()` and not
  hidden in the production profile (`productionHiddenToolNames`), no duplicates,
  at most 10 entries, and the JSON of the core definitions is at most 40 KB
  (40 * 1024 bytes). The test logs the measured size on every run. Today it is
  13554 bytes for 10 tools.
- `TestCoreToolNamesEmptyInScaffold` is deleted, as the procedure says.
- #8759 stays open. The 14-day criterion is not met: the data covers about 7
  days.

### Measurement procedure

1. Pick a hosted deployment and note only its class here, for example
   `<production-profile | QA>`. Write no hostname, account name, or absolute
   volume.
2. Read the configured metrics retention and record it as `<days>`. Choose a
   window of at least 14 days. If retention is shorter, record the shorter
   window and say so.
3. Confirm the series name first, because OTLP-to-Prometheus translation can
   add suffixes:

   ```promql
   {__name__=~"eshu_dp_mcp_response_bytes.*"}
   ```

4. Run the share query. The metric is a histogram, so `_count` is one increment
   per sized response. Keep the `{pod!=""}` selector if the datasource returns
   the series twice, as the QA one does; without it every call is counted in
   both variants. Drop the selector on a datasource that has a single variant.

   ```promql
   sum by (tool) (increase(eshu_dp_mcp_response_bytes_count{pod!=""}[14d]))
     / ignoring(tool) group_left
   sum(increase(eshu_dp_mcp_response_bytes_count{pod!=""}[14d]))
   ```

5. Run the error-path check for tools that exceed the response budget:

   ```promql
   sum by (tool) (increase(eshu_dp_mcp_response_over_budget_total{pod!=""}[14d]))
   ```

6. Paste shares only, as percentages of traffic, into the table below.
7. Cross-check against the #8756 corpus, then prefer canonical family members
   from #8754, then pick at most 10.
8. Check each chosen name against the production-filtered tool list
   (`productionHiddenToolNames` in `go/internal/mcp`). A registered tool can be
   unadvertised in the production profile, so a name that passes
   `ReadOnlyTools()` can still be hidden there. The PR that fills the list owns
   this check.

| Tool | Share of traffic | Reason it made the list |
|---|---|---|
| `list_codeowners_ownership` | 29.6% | Rank 1 by share. Not in a #8754 family. QA may be test-driver heavy. |
| `search_file_content` | 19.0% | Rank 2 by share. Non-canonical `code-lookup` member (see the canonical-family check). |
| `get_file_lines` | 12.4% | Rank 3 by share. `source-read` member; its canonical `get_file_content` is also chosen. |
| `get_repository_freshness` | 6.5% | Rank 4 by share. Non-canonical `freshness` member (see the canonical-family check). |
| `get_file_content` | 3.5% | Rank 5 by share. Canonical `source-read` member. |
| `list_repository_files` | 3.4% | Rank 6 by share. `source-read` member; its canonical is also chosen. |
| `find_code` | 2.6% | Rank 7 by share. Non-canonical `code-lookup` member (see the canonical-family check). |
| `list_repositories_by_language` | 2.6% | Rank 8 by share. Canonical `repository-language` member. |
| `find_infra_resources` | 2.4% | Rank 9 by share. Canonical `infra-resource-search` member. |
| `investigate_code_topic` | 2.4% | Rank 10 by share. Non-canonical `code-lookup` member (see the canonical-family check). |

Deployment class: `QA`. Window: samples on 6 days between 17 and 23 October
2026 (about 7 days, not 14). Retention: `NOT_CHECKED`.

### Checks applied to the top 10

| Rule | Result |
|---|---|
| Every name is in `ReadOnlyTools()` | Pass for all 10 (`TestCoreRouterToolSetGuardrails`). |
| None is hidden in the production profile | Pass. `productionHiddenToolNames` holds the divergence family plus `compare_code_paths`; none is in the list (`TestCoreRouterToolsAreAdvertisedInProduction`). |
| Cross-check with the #8756 corpus (200 questions, 166 expected tools) | Pass. All 10 appear as expected or alternate: `list_codeowners_ownership` 1 expected, `search_file_content` 2 + 1 alternate, `get_file_lines` 1 + 1, `get_repository_freshness` 1, `get_file_content` 2 + 1, `list_repository_files` 2, `find_code` 2 + 8, `list_repositories_by_language` 1, `find_infra_resources` 2 + 1, `investigate_code_topic` 2 + 1. |
| Size guard, 40 KB | Pass. 13554 bytes for 10 definitions. |
| At most 10 entries | Pass. Exactly 10. |

### Deviations from top 10 by share

None. The list is the default.

### Canonical-family check (#8754)

Four chosen tools are non-canonical members of a family whose canonical member
is not on the list. The list was not changed for them; each is a candidate swap
for the refresh, not applied here:

| Chosen tool | Family | Canonical member not chosen | Note |
|---|---|---|---|
| `search_file_content` | `code-lookup` | `find_symbol` | `find_symbol` had no samples in this window. |
| `find_code` | `code-lookup` | `find_symbol` | Same family, same swap. |
| `investigate_code_topic` | `code-lookup` | `find_symbol` | Same family, same swap. |
| `get_repository_freshness` | `freshness` | `get_freshness_causality` | `get_freshness_causality` had no samples in this window. |

`get_file_lines` and `list_repository_files` are non-canonical `source-read`
members, but their canonical `get_file_content` is on the list.
`list_codeowners_ownership` is in no family. Swapping a measured tool for an
unmeasured canonical one would put opinion back into the list, so the choice
for the refresh is to swap only when traffic for the canonical member exists.

## Bias in the series

`eshu_dp_mcp_response_bytes` is recorded by `recordResponseBytes`, called from
`applyResponseBudget` in `go/internal/mcp/dispatch_budget.go`, after the
`budget <= 0` early return. That call runs only after a dispatch produced a
result. These calls leave no sample:

| Missing from the series | Where |
|---|---|
| Timeouts and canceled dispatches | `dispatchToolWithOptions` returns `dispatchContextError` at its context checks, before the budget runs |
| Unknown tool names | `resolveRoute` returns `unknown tool` |
| HTTP 400 and above whose body is not a canonical envelope | `dispatchToolWithOptions` returns an `HTTP <code>` error |
| Every call, when the response budget is disabled | `applyResponseBudget` returns at the `budget <= 0` early return, before `recordResponseBytes` |

Error responses that carry a canonical envelope are counted:
`dispatchToolWithOptions` sets `IsError` and still passes them to
`applyResponseBudget`. A deadline-truncated partial answer is counted too
(`deadlinePartialDispatchResult` calls `applyResponseBudget`).

Direction of the bias (a theory, not measured): agents that pick the wrong tool
tend to hit unknown-name or timeout paths, so the counts under-state
misrouted traffic, and a tool that often times out looks less used than it is.
The gap cannot be sized from metrics today, because no counter exists at
dispatch entry.

Sample bias: the QA sample may be test-driver heavy. `list_codeowners_ownership`
is the top tool at 29.6 percent, and a production-class deployment may rank the
tools differently.

Rule for the refresh: if the missing paths could change which ten tools make
the list (for example a high-timeout tool sits near the cut line), add a
counter at dispatch entry first, collect, and only then choose. That counter is
a separate telemetry change and needs its own review.

## Consequences

Good:

- `core_router` can skip the `find_tool` round trip for about 84 percent of
  sampled calls these tools carry (a share of the sample, not a measured
  latency saving).
- The 10-tool, 40 KB, production-visibility, and exact-list checks fail a PR
  that breaks them.
- #8760 can now compare `router` and `core_router`.
- The refresh is one documented query, so anyone with monitoring access can
  finish this.

Bad:

- The list rests on a sparse QA sample that may be test-driver heavy, so it may
  not match production-profile traffic.
- Three `code-lookup` tools sit on the list instead of their canonical
  `find_symbol`; the effect on first-try tool choice is not measured here.
- Refresh with a 14-day window on a production-class deployment stays open on
  #8759.

## Implementation plan

- `go/internal/mcp/router/core.go` holds the list with a reason per tool.
- The tests live in `go/internal/mcp` because `go/internal/mcp/router` must not
  import the MCP root (`TestRouterDoesNotImportMCPRoot`).
- On refresh, change only `coreToolNames`, `measuredCoreToolNames`, and this
  record's table.
- Operator signal for the chosen list: the same `eshu_dp_mcp_response_bytes`
  series, by `tool`, before and after the surface switch.

No-Regression Evidence: advertisement only. `tools/call` dispatches every
registered tool whatever the surface, so no query, graph write, or queue path
changes. The core set adds 13554 bytes to the `core_router` `tools/list`
result, under the 40 KB limit (`TestCoreRouterToolSetGuardrails`).

No-Observability-Change: the signal is the existing
`eshu_dp_mcp_response_bytes` histogram by `tool`, and the startup log
`mcp tool surface` already reports `tool_count` and `tools_list_bytes`.

## Acceptance criteria

- [x] A test fails when `coreToolNames` names an unregistered tool or holds
      more than 10 entries.
- [x] A test reports the serialized size of the core set and fails above 40 KB.
- [x] This record includes the exact PromQL, so the list can be refreshed.
- [x] This record contains no deployment hostnames, account names, or employer
      identifiers.
- [ ] The query was run on a hosted deployment over at least 14 days. NOT met:
      the data is a QA sample with samples on 6 days between 17 and 23 October
      2026 (about 7 days), and retention is NOT_CHECKED. The shares are in the
      table above.
- [x] The chosen list, at most 10 tools, is in `core.go`, with a reason per
      tool.
- [x] A test pins the chosen list and fails when a name is hidden in
      production, unregistered, duplicated, over 10, or over 40 KB.
- [x] `TestCoreToolNamesEmptyInScaffold` is deleted.

<details>
<summary>Evidence and measurements</summary>

Commands (run from `go/`):

```bash
go test ./internal/mcp/ -run 'TestCoreRouterToolSet|TestCoreRouterTools|TestCoreSetProblemsFlagsSeededViolations' -count=1 -v
```

The guardrail test logs `core_router core set: N tools, B serialized bytes`.
With the list filled it reports 10 tools and 13554 bytes (limit 40960).

Series check and per-tool query, from the measurement procedure above (run by
the orchestrator against the QA datasource, one instant query at the end of the
window). The shares in the table are each tool's sum from the second query
divided by the total of the same query, computed outside PromQL, which is the
ratio form in step 4:

```promql
{__name__=~"eshu_dp_mcp_response_bytes.*"}
sum by (tool) (increase(eshu_dp_mcp_response_bytes_count{pod!=""}[14d]))
```

Corpus cross-check, from the repository root:

```bash
python3 -I -c "import json;c=json.load(open('go/internal/mcp/router/eval/testdata/corpus.json'))['questions'];print(len(c),len({q['expected'] for q in c}))"
```

It prints `200 166`.

Headroom check at base `cb8a2d0845` (a theory about today's registry, derived
by marshaling each `ReadOnlyTools()` definition and sorting by size): the ten
largest definitions together stay under 40 KB, so the size limit does not bind
for any 10-tool set today. It starts to bind only if definitions grow. An
11-tool set of the largest definitions does exceed it.

Phase 1 access probes, all read-only: `aws sts get-caller-identity` (dev box
role), namespace and workload listing in the app namespace, a self-access review
for `services/proxy` in the monitoring and app namespaces, and three scrape
attempts against the MCP server metrics endpoints that timed out.

</details>
