# Advertise ten measured tools on core_router (#8759)

Status: Accepted for the 21-day QA list below. The guardrails and the first
procedure were accepted by the arbiter ruling on #8759 (2026-10-22). The
refresh rule (per-day normalised shares over a window of at least 14 days) was
ruled by the arbiter on 2026-10-23 and ran on that day. The 14-day criterion is
met, so #8759 closes with this change and the traffic-concentration caveat below. A
production-class refresh is a follow-up the owner can open. This record does
not approve a default-surface change (#8760).

**`core_router` advertises 10 tools chosen by mean per-day share of measured MCP
calls over a 21-day QA window, beside `find_tool` and `call_tool`.** The list
is in `coreToolNames` in `go/internal/mcp/router/core.go`. It came from call
counts and a pre-registered rule, not from docs or opinion. The traffic is
driven by a few days that look like test-driver runs and is not a measure of
assistant usage. Real-agent measurement is #8760. The list is a weak signal;
see Sensitivity.

`List: QA, window 2026-10-02 to 2026-10-22 (21 complete UTC days, 14 active days); one canonical-family swap`

## Context

Under the `router` surface every new kind of question costs one more call.
Advertising about ten of the most-used tools directly removes that cost for
most traffic. Epic #8752 requires the list to come from measured per-tool call
counts, because a guessed list also contaminates the #8760 before and after
comparison. Facts, each with its source:

- Series: `eshu_dp_mcp_response_bytes_count`, label `tool`, from the QA
  metrics datasource. This datasource returns a single series variant, so no
  `{pod!=""}` selector is used. The earlier 7-day sample came from a second
  datasource that returns every series twice and needed that selector.
- Deployment class: QA. The datasource has no cluster label. QA is inferred
  because the scrape targets match the QA MCP pods.
- Traffic is concentrated on a few days that look like test-driver runs: the 4
  busiest days carry 87 percent of the counted calls. Whole-window share is
  therefore not the selector, because a handful of days would set the list.
  Whole-window share stays in the table because the issue asks for shares of
  traffic.
- The two datasources roughly agreed on the overlapping 7-day window (largest
  gap 1.1 points), which supports the earlier sample and the single-variant
  choice: `list_codeowners_ownership` 29.7 vs 29.6 percent,
  `search_file_content` 17.9 vs 19.0, `get_file_lines` 12.4 on both,
  `get_repository_freshness` 6.5 on both. The four paired figures came from
  one instant query, `sum by (tool)(increase(eshu_dp_mcp_response_bytes_count[7d]))`,
  run on each datasource on 2026-10-23. They are not in the committed data
  files.
- Retention: measured, not configured. The datasource used holds at least 31
  days of samples (its `up` series begins 2026-09-22) and holds this MCP series
  from 2026-10-01; the configured retention setting was not readable from the
  dev box. The earlier sample came from a second datasource that holds about 8
  days. Window used: 2026-10-02 to 2026-10-22, 21 complete UTC days (the series
  starts on 2026-10-01, a partial day).
- 14 of the 21 days are active (at least 10 counted calls). The other 7 days
  carry too little traffic to count.
- Absolute volumes stay out of this record on purpose.
- The epic's 163-tool figure came from a production-profile deployment, so
  this QA sample may not match it.

## Options

| Option | Cost | Verdict |
|---|---|---|
| Pick ten tools from docs, intents, or playbooks | Cheap | Rejected. It is the opinion the issue rules out and it contaminates #8760. |
| Rank by whole-window share | A handful of busy days sets the list; `get_service_context` alone would take 30 percent | Rejected as the selector. Kept as a reported column. |
| Rank by mean per-day share over active days, swap in canonical family members | Equal weight per active day stops one huge day from setting the top ranks. Ranks 5 to 10 still rest on two or three days each, and removing one active day can change the set (see Sensitivity) | Chosen. It follows the arbiter's rule. |
| Wait for a production-class sample | `core_router` stays unmeasured and #8760 cannot compare it with `router` | Rejected for now. It is a follow-up. |

## Decision

- `coreToolNames` holds the 10 tools in the table below.
  `TestCoreRouterToolSetMatchesMeasuredList` pins `core.go` to
  `measuredCoreToolNames` in `go/internal/mcp/core_tool_set_test.go`, so a list
  change needs a matching change to this table and to that slice.
- The list stays within three limits, enforced by
  `TestCoreRouterToolSetGuardrails`: every name is in `ReadOnlyTools()` and not
  hidden in the production profile (`productionHiddenToolNames`), no
  duplicates, at most 10 entries, and the JSON of the core definitions is at
  most 40 KB (40 * 1024 bytes). The test logs the measured size on every run.
  Today it is 16176 bytes for 10 tools.
- The refresh closes #8759. The caveats below stay attached to the list.

### Measurement procedure (the refresh rule)

1. Window: all complete UTC days the datasource holds for
   `eshu_dp_mcp_response_bytes_count`, ending at the last UTC midnight before
   the refresh query. It must span at least 14 calendar days. Drop the partial
   current day and the partial first day. Record start, end, and day count.
   Here: 2026-10-02 to 2026-10-22, 21 days. Write only a deployment class, no
   hostname, account name, or absolute volume.
2. Daily counts: a range query, one step per day, evaluated at each UTC
   midnight in the window. Keep a `{pod!=""}` selector only on a datasource
   that returns each series twice. This datasource returns one variant, so the
   selector was not used.

   ```promql
   sum by (tool) (increase(eshu_dp_mcp_response_bytes_count[1d]))
   ```

   Confirm the series name first, because OTLP-to-Prometheus translation can
   add suffixes: `{__name__=~"eshu_dp_mcp_response_bytes.*"}`.
3. Active day: a UTC day whose total across tools is at least 10. Report the
   active-day count. Fewer than 14 means the refresh does not ship. Here: 14.
4. Daily share is the tool's count divided by that day's total, on each active
   day. Score is the arithmetic mean of the daily share over all active days; a
   day with no calls counts 0. Report percent to one decimal.
5. Eligibility: at least one call on at least 2 distinct active days. A tool
   seen on one day is ineligible.
6. Rank by score, descending. A tie at one decimal goes to more distinct active
   days, then higher whole-window share, then a canonical family member, then
   tool name ascending.
7. Canonical swap: for each #8754 family with a non-canonical member in the top
   10 whose canonical member is eligible but outside the top 10, replace that
   family's lowest-ranked non-canonical member with the canonical member. One
   swap per family. Record each swap under Deviations. The canonical member is
   the first entry of the family's `Members` slice in
   `go/internal/ask/catalog/annotations_intents_families.go`.
8. Take the first 10 after the swap. Record ranks 11 to 13 with scores. Run the
   over-budget check as a recorded check, not a reason to drop a tool:

   ```promql
   sum by (tool) (increase(eshu_dp_mcp_response_over_budget_total[1d]))
   ```

9. Table columns: tool, per-day score (the selection basis), whole-window
   share, distinct active days. Write no absolute volume.
10. Check each chosen name against the production-filtered tool list
    (`productionHiddenToolNames` in `go/internal/mcp`). A registered tool can
    be unadvertised in the production profile. The PR that fills the list owns
    this check.

### The list

| Rank | Tool | Per-day score | Whole-window share | Active days | Note |
|---|---|---|---|---|---|
| 1 | `search_file_content` | 26.3% | 29.7% | 8 | Non-canonical `code-lookup` member; canonical `find_symbol` is ineligible. |
| 2 | `get_service_context` | 10.4% | 30.2% | 3 | Non-canonical `service-workload` member; canonical `get_service_story` is ineligible. |
| 3 | `get_file_content` | 5.5% | 6.6% | 5 | Canonical `source-read` member. |
| 4 | `find_cross_repo_dead_code` | 5.0% | 10.8% | 3 | `dead-code` member; canonical `investigate_dead_code` is also chosen. |
| 5 | `search_semantic_context` | 4.9% | 0.5% | 2 | In no #8754 family. |
| 6 | `investigate_dead_code` | 3.5% | 10.0% | 3 | Canonical `dead-code` member. |
| 7 | `plan_developer_change` | 3.3% | 0.4% | 2 | Non-canonical `change-impact` member; canonical `investigate_change_surface` is ineligible. |
| 8 | `get_file_lines` | 2.5% | 1.0% | 2 | `source-read` member; canonical `get_file_content` is also chosen. Wins the tie with `analyze_pre_change_impact` on whole-window share. |
| 9 | `analyze_pre_change_impact` | 2.5% | 0.3% | 2 | Non-canonical `change-impact` member; canonical `investigate_change_surface` is ineligible. |
| 10 | `get_repo_story` | 0.1% | 0.2% | 2 | Swap: canonical `repository-overview` member, replaces `get_repo_context`. Listed last, out of score order. |

The 10 tools together carry about 90 percent of the whole-window calls (the
rounded shares sum to 89.7). 87 percent of counted calls fall on the four
busiest active days. Their per-day
scores sum to 64.0 percent.

Ranks 11 to 13, outside the list:

| Rank | Tool | Per-day score | Whole-window share | Active days |
|---|---|---|---|---|
| 11 | `find_change_surface` | 2.4% | 0.3% | 2 |
| 12 | `search_entity_content` | 2.2% | 0.2% | 4 |
| 13 | `list_ci_cd_run_correlations` | 2.2% | 0.3% | 2 |

Ranks 12 and 13 tie at one decimal. `search_entity_content` ranks first on
more active days (4 against 2).

### Checks applied to the list

| Rule | Result |
|---|---|
| Every name is in `ReadOnlyTools()` | Pass for all 10 (`TestCoreRouterToolSetGuardrails`). |
| None is hidden in the production profile | Pass for all 10 (`TestCoreRouterToolsAreAdvertisedInProduction`). |
| Cross-check with the #8756 corpus (200 questions, 166 expected tools) | Pass. All 10 appear as expected or alternate: `search_file_content` 2 + 1 alternate, `get_service_context` 1 + 2, `get_file_content` 2 + 1, `find_cross_repo_dead_code` 2 + 0, `search_semantic_context` 1 + 2, `investigate_dead_code` 2 + 3, `plan_developer_change` 1 + 1, `get_file_lines` 1 + 1, `analyze_pre_change_impact` 1 + 2, `get_repo_story` 1 + 1. |
| Size guard, 40 KB | Pass. 16176 bytes for 10 definitions. |
| At most 10 entries | Pass. Exactly 10. |
| Over-budget responses (step 8) | Carried forward from the earlier 7-day check for tools still in the list: `search_file_content`, `get_file_content`, and `get_file_lines` had none there. The other seven are NOT_CHECKED on this window; the dev box could not rerun the query. This is a reason to recheck, not a reason to drop a tool. |

### Deviations

One canonical swap (step 7). Step 7 was checked for every family in
`go/internal/ask/catalog/annotations_intents_families.go`, not only the three
the earlier sample flagged. Only `repository-overview` triggers it:

- `repository-overview` has canonical `get_repo_story`, which is eligible (2
  active days, score 0.1 percent, rank 25) and outside the top 10. The family's
  non-canonical `get_repo_context` is in the top 10 at rank 8 (2.8 percent, 4
  active days). `get_repo_context` is replaced by `get_repo_story`.
  The swap trades a measured score for a canonical pick by a rule fixed before
  the numbers were known.

Family-by-family result of the step 7 check (canonical member, first in the
family's `Members`):

| Family | Canonical member | Eligible? | Result |
|---|---|---|---|
| `code-lookup` | `find_symbol` | No, calls on one day | No swap. |
| `source-read` | `get_file_content` | Yes, rank 3 | Already in the top 10. |
| `code-relationships` | `get_code_relationship_story` | No, calls on one day | No member in the top 10. |
| `dead-code` | `investigate_dead_code` | Yes, rank 6 | Already in the top 10. |
| `code-quality` | `inspect_code_quality` | No, calls on one day | No member in the top 10. |
| `code-divergence` | `report_code_divergence` | No, calls on one day | No member in the top 10. |
| `repository-overview` | `get_repo_story` | Yes, rank 25 | Swap, see above. |
| `ecosystem-overview` | `get_ecosystem_overview` | No, calls on one day | No member in the top 10. |
| `repository-language` | `list_repositories_by_language` | Yes, rank 23 | No member in the top 10. |
| `service-workload` | `get_service_story` | No, calls on one day | No swap. |
| `change-impact` | `investigate_change_surface` | No, calls on one day | No swap. |
| `changed-since` | `get_changed_since` | No, calls on one day | No member in the top 10. |
| `deployment` | `trace_deployment_chain` | No, calls on one day | No member in the top 10. |
| `infra-resource-search` | `find_infra_resources` | Yes, rank 19 | No member in the top 10. |
| `infra-resource-trace` | `investigate_resource` | No, calls on one day | No member in the top 10. |
| `vulnerability-impact` | `list_supply_chain_impact_findings` | No, calls on one day | No member in the top 10. |
| `ci-cd` | `list_ci_cd_run_correlations` | Yes, rank 13 | No member in the top 10. |
| `documentation-findings` | `list_documentation_findings` | Yes, rank 28 | No member in the top 10. |
| `iac-management` | `find_unmanaged_resources` | No, calls on one day | No member in the top 10. |
| `replatforming` | `compose_replatforming_plan` | No, calls on one day | No member in the top 10. |
| `runtime-health` | `get_operator_control_plane` | No, calls on one day | No member in the top 10. |
| `ingester-status` | `list_ingesters` | No, calls on one day | No member in the top 10. |
| `capability-catalog` | `get_capability_catalog` | No, calls on one day | No member in the top 10. |
| `freshness` | `get_freshness_causality` | No, calls on one day | No freshness member is in the top 10, so no swap. |
| 10 further families | various | No eligible canonical member | No member in the top 10. |

The 10 further families have no eligible canonical member:
`security-alert-reconciliation`, `sbom-attestation`,
`container-image-identity`, `package-registry-packages`,
`secrets-iam-posture`, `runtime-drift`, `extraction-readiness`, `fact-schema`,
`call-plans`, and `failed-work`. Their canonical members have no calls. Three
have a non-canonical member with calls on a single day:
`package-registry-packages` (`search_registry_bundles`), `runtime-drift`
(`list_aws_runtime_drift_findings`), and `call-plans` (`list_query_playbooks`).
That does not change any swap.

The canonical members `find_symbol`, `get_service_story`, and
`get_freshness_causality` each have calls on one day only, so they are
ineligible. No swap applies to `code-lookup` or `service-workload`, and no
`freshness` member is in the top 10. `investigate_change_surface` is also single-day, so no swap
applies to `change-impact`.

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
dispatch entry. The dispatch-entry bias is not sized: NOT_CHECKED.

The gap between ranks 10, 11, 12 and 13 (2.5, 2.4, 2.2, 2.2) is small enough
that the unsized bias could change the list. The counter is deferred to the
next refresh. The merge review of this record is asked to rule on that.

Rule for the next refresh: if the missing paths could change which ten tools
make the list (for example a high-timeout tool sits near the cut line), add a
counter at dispatch entry first, collect, and only then choose. That counter is
a separate telemetry change and needs its own review.

## Caveats

- Concentrated traffic. The 4 busiest days carry 87 percent of the counted
  calls and look like test-driver runs. The list is driven by a few such days
  and is not a measure of assistant usage. Real-agent measurement is #8760.
- The earlier top tool, `list_codeowners_ownership`, has calls on one day only
  in this window, so it is ineligible. A single busy day ranked first in the
  7-day sample; the per-day score stops that.
- QA, not production. A production-class refresh is a follow-up the owner can
  open. It does not block this change.
- The swap puts `get_repo_story` (0.1 percent) on the list. Its effect on
  first-try tool choice is not measured here.

## Sensitivity

The list is a weak signal. Percentages only; the leave-one-day-out figures are
computed from the one-decimal per-day share matrix in the details block, with
tools outside the matrix assumed unable to reach the cut line (their best
score is 1.2).

Share of each tool's score that comes from its single biggest day:

| Tool | Biggest day's share of its score |
|---|---|
| `search_semantic_context` | 97% |
| `find_cross_repo_dead_code` | 65% |
| `plan_developer_change` | 62% |
| `get_service_context` | 60% |
| `analyze_pre_change_impact` | 54% |
| `get_file_lines` | 52% |
| `investigate_dead_code` | 50% |
| `search_file_content` | 27% |
| `get_file_content` | 27% |

Leave one active day out and rerun steps 3 to 6. The set of ten changes for 9
of the 14 active days:

| Day dropped (UTC, 2026) | Leaves the top ten | Enters |
|---|---|---|
| 10-03 | `investigate_dead_code` | `find_change_surface` |
| 10-04 | `find_cross_repo_dead_code`, `investigate_dead_code` | `find_change_surface`, `search_entity_content` |
| 10-05 | `plan_developer_change`, `analyze_pre_change_impact` | `search_entity_content`, `find_dead_code` |
| 10-06 | `plan_developer_change`, `analyze_pre_change_impact` | `search_entity_content`, `find_dead_code` |
| 10-07 | `get_repo_context` (the swapped-out tool, so the swap no longer applies) | `find_change_surface` |
| 10-11 | `search_semantic_context` | `find_change_surface` |
| 10-12 | `get_file_lines` | `find_change_surface` |
| 10-15 | `search_semantic_context` | `find_change_surface` |
| 10-22 | `get_file_lines` | `find_change_surface` |

Dropping any of the other 5 active days leaves the set unchanged.

Five of the 10 tools have a whole-window share of 1.0 percent or less:
`search_semantic_context` 0.5, `plan_developer_change` 0.4, `get_file_lines`
1.0, `analyze_pre_change_impact` 0.3, and `get_repo_story` 0.2. A day with few
calls weighs the same as a day with thousands. That is the rule working as
registered, and it is why the list is weak. #8760's real-agent measurement is
the check.

## Consequences

Good:

- `core_router` can skip the `find_tool` round trip for the tools these
  scores favor. This is a share of the sample, not a measured latency saving.
- Equal weight per active day stops one huge day from setting the top ranks,
  and the rule is written down, so a later refresh follows the same steps.
- The 10-tool, 40 KB, production-visibility, and exact-list checks fail a PR
  that breaks them.
- #8760 can compare `router` and `core_router`.

Bad:

- The list rests on QA traffic from a few days that look like test-driver
  runs, so it may not match production-profile traffic or real assistant use.
- One list member is there by rule, not by traffic (`get_repo_story`).
- The over-budget check was not rerun for seven tools.

## Implementation plan

- `go/internal/mcp/router/core.go` holds the list with a reason per tool.
- The tests live in `go/internal/mcp` because `go/internal/mcp/router` must not
  import the MCP root (`TestRouterDoesNotImportMCPRoot`).
- On refresh, change only `coreToolNames`, `measuredCoreToolNames`, and this
  record's tables, plus the prose that quotes them.
- Operator signal for the chosen list: the same `eshu_dp_mcp_response_bytes`
  series, by `tool`, before and after the surface switch.

No-Regression Evidence: advertisement only. `tools/call` dispatches every
registered tool whatever the surface, so no query, graph write, or queue path
changes. The core set adds 16176 bytes to the `core_router` `tools/list`
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
- [x] The query was run on a hosted deployment over at least 14 days: 21
      complete UTC days, 14 active days, QA class, with the traffic-concentration
      caveat above.
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
With this list it reports 10 tools and 16176 bytes (limit 40960).

Corpus cross-check, from the repository root:

```bash
python3 -I -c "import json;c=json.load(open('go/internal/mcp/router/eval/testdata/corpus.json'))['questions'];print(len(c),len({q['expected'] for q in c}))"
```

It prints `200 166`.

Headroom check at base `cb8a2d0845` (a theory about that registry, derived by
marshaling each `ReadOnlyTools()` definition and sorting by size): the ten
largest definitions together stay under 40 KB, so the size limit does not bind
for any 10-tool set today. It was not re-run for this refresh.

Per-day score table, the top 20 eligible tools (mean daily share over the 14
active days). `get_repo_story` sits at
rank 25 with a score of 0.1 percent, 0.2 percent whole-window share, and 2
active days.

| Rank | Tool | Per-day score | Whole-window share | Active days |
|---|---|---|---|---|
| 1 | `search_file_content` | 26.3% | 29.7% | 8 |
| 2 | `get_service_context` | 10.4% | 30.2% | 3 |
| 3 | `get_file_content` | 5.5% | 6.6% | 5 |
| 4 | `find_cross_repo_dead_code` | 5.0% | 10.8% | 3 |
| 5 | `search_semantic_context` | 4.9% | 0.5% | 2 |
| 6 | `investigate_dead_code` | 3.5% | 10.0% | 3 |
| 7 | `plan_developer_change` | 3.3% | 0.4% | 2 |
| 8 | `get_repo_context` | 2.8% | 0.4% | 4 |
| 9 | `get_file_lines` | 2.5% | 1.0% | 2 |
| 10 | `analyze_pre_change_impact` | 2.5% | 0.3% | 2 |
| 11 | `find_change_surface` | 2.4% | 0.3% | 2 |
| 12 | `search_entity_content` | 2.2% | 0.2% | 4 |
| 13 | `list_ci_cd_run_correlations` | 2.2% | 0.3% | 2 |
| 14 | `find_dead_code` | 2.0% | 1.6% | 3 |
| 15 | `list_repository_files` | 1.7% | 0.4% | 6 |
| 16 | `find_code` | 1.3% | 0.2% | 2 |
| 17 | `get_repo_summary` | 1.2% | 0.2% | 4 |
| 18 | `investigate_code_topic` | 1.2% | 0.1% | 2 |
| 19 | `find_infra_resources` | 1.1% | 0.2% | 3 |
| 20 | `list_indexed_repositories` | 0.9% | 0.2% | 7 |

Per-day share matrix, percent of that day's counted calls, for the tools the
rule tracked. Columns are UTC days in 2026 (MM-DD); the other 7 days of the
window are not active. A zero means no calls that day. `get_repo_story` is not
in this matrix; its score comes from the ranking above.

| Tool | 10-02 | 10-03 | 10-04 | 10-05 | 10-06 | 10-07 | 10-08 | 10-11 | 10-12 | 10-13 | 10-15 | 10-17 | 10-20 | 10-22 |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| `search_file_content` | 0.0 | 0.0 | 0.0 | 21.5 | 0.0 | 0.0 | 61.7 | 98.1 | 54.2 | 81.2 | 1.5 | 0.0 | 31.3 | 19.2 |
| `get_service_context` | 87.0 | 51.5 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 6.7 | 0.0 | 0.0 |
| `get_file_content` | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 19.1 | 0.0 | 20.7 | 18.2 | 0.0 | 0.0 | 16.4 | 2.0 |
| `find_cross_repo_dead_code` | 3.4 | 21.0 | 45.3 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 |
| `search_semantic_context` | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 1.9 | 0.0 | 0.0 | 67.2 | 0.0 | 0.0 | 0.0 |
| `investigate_dead_code` | 3.4 | 20.9 | 24.5 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 |
| `plan_developer_change` | 0.0 | 0.0 | 0.0 | 28.6 | 17.2 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 |
| `get_repo_context` | 0.0 | 0.0 | 0.0 | 0.0 | 10.9 | 23.6 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 4.5 | 0.6 |
| `get_file_lines` | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 16.7 | 0.0 | 0.0 | 0.0 | 0.0 | 18.1 |
| `analyze_pre_change_impact` | 0.0 | 0.0 | 0.0 | 15.7 | 18.8 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 |
| `find_change_surface` | 0.0 | 0.0 | 0.0 | 15.7 | 17.2 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 |
| `search_entity_content` | 0.1 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.9 | 0.0 | 22.4 | 8.0 | 0.0 | 0.0 |
| `list_ci_cd_run_correlations` | 0.0 | 0.0 | 0.0 | 15.7 | 14.9 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 |
| `find_dead_code` | 3.5 | 1.0 | 23.9 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 |
| `list_repository_files` | 0.0 | 0.5 | 0.0 | 0.0 | 0.0 | 0.0 | 0.7 | 0.0 | 0.9 | 0.0 | 0.0 | 9.4 | 12.0 | 0.6 |
| `find_code` | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 1.3 | 0.0 | 0.0 | 17.1 | 0.0 | 0.0 |
| `find_symbol` | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 |
| `get_service_story` | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 10.7 | 0.0 | 0.0 |
| `get_freshness_causality` | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 |
| `list_codeowners_ownership` | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 43.3 |
| `get_repository_freshness` | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 9.5 |

Earlier 7-day sample, kept as a labelled prior sample. It was taken on
2026-10-23 from the second datasource, over samples on 6 days between 17 and 23
October 2026, with the `{pod!=""}` selector. It picked the previous list and is
superseded by the table above:

| Tool | Share of traffic |
|---|---|
| `list_codeowners_ownership` | 29.6% |
| `search_file_content` | 19.0% |
| `get_file_lines` | 12.4% |
| `get_repository_freshness` | 6.5% |
| `get_file_content` | 3.5% |
| `list_repository_files` | 3.4% |
| `find_code` | 2.6% |
| `list_repositories_by_language` | 2.6% |
| `find_infra_resources` | 2.4% |
| `investigate_code_topic` | 2.4% |

Prior sample over-budget check: `get_service_story`, `list_indexed_repositories`,
and `list_repository_files` had over-budget responses. Only
`list_repository_files` was in that list, at about 6 percent of its own calls.

Phase 1 access probes, all read-only: `aws sts get-caller-identity` (dev box
role), namespace and workload listing in the app namespace, a self-access review
for `services/proxy` in the monitoring and app namespaces, and three scrape
attempts against the MCP server metrics endpoints that timed out.

</details>
