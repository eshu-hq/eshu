# Hold the core_router tool list empty until measured call counts exist (#8759)

Status: Interim. The guardrails and the measurement procedure are accepted
(arbiter ruling on #8759, 2026-10-22). The core list is NOT_PROVEN and #8759
stays open.

**`coreToolNames` stays empty, so `core_router` behaves like `router`, until
someone runs the query below on a hosted deployment and records the shares here.
The guardrail tests that bound the eventual list ship now.**

`List: NOT_PROVEN, pending data`

## Context

Under the `router` surface every new kind of question costs one more call.
Advertising about ten of the most-used tools directly removes that cost for
most traffic. Epic #8752 requires the list to come from measured per-tool call
counts, not from opinion, because a guessed list also contaminates the #8760
before and after comparison.

Phase 1 of this work could not get the counts. Facts, each with its source:

- The history lives in the monitoring stack of the QA cluster. The dev box role
  gets a 403 on `services/proxy` there. A self-access review confirms it
  (`allowed=false`). Retention is NOT_CHECKED because its settings sit behind
  the same 403.
- The MCP pods export metrics over OTLP to a collector. The pod's own
  `/metrics` endpoint was unreachable through the API server (requests timed
  out), and its counters would cover about one day of pod life.
- The QA deployment may not match real use: its traffic could be mostly test
  drivers. The epic's 163-tool figure came from a production-profile
  deployment.
- The cross-check against the #8756 eval corpus is NOT_CHECKED (the corpus does
  not exist yet). The canonical-family preference from #8754 is NOT_CHECKED for
  the same reason.

## Options

| Option | Cost | Verdict |
|---|---|---|
| Pick ten tools from docs, intents, or playbooks | Cheap | Rejected. It is the opinion the issue rules out and it contaminates #8760. |
| Ship guardrail tests and this record, list empty | The surface gains nothing until data lands | Chosen. The limits are enforced and the refresh procedure is written. |
| Wait and ship nothing | No review cost now | Rejected. The guardrails and the bias analysis do not depend on data. |

## Decision

- Keep `coreToolNames` empty in `go/internal/mcp/router/core.go`.
- Hold any future list to three limits, enforced by
  `TestCoreRouterToolSetGuardrails` in `go/internal/mcp`:
  every name is in `ReadOnlyTools()`, no duplicates, at most 10 entries, and
  the JSON of the core definitions is at most 40 KB (40 * 1024 bytes). The
  test logs the measured size on every run.
- #8759 stays open. Acceptance criterion 3 (the PromQL was actually run) is not
  met by this record.

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
   per sized response:

   ```promql
   sum by (tool) (increase(eshu_dp_mcp_response_bytes_count[14d]))
     / ignoring(tool) group_left
   sum(increase(eshu_dp_mcp_response_bytes_count[14d]))
   ```

5. Run the error-path check for tools that exceed the response budget:

   ```promql
   sum by (tool) (increase(eshu_dp_mcp_response_over_budget_total[14d]))
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
| (no data yet) | NOT_PROVEN | |

Deployment class: `<fill in>`. Window: `<fill in>`. Retention: `<fill in>`.

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

Rule for the refresh: if the missing paths could change which ten tools make
the list (for example a high-timeout tool sits near the cut line), add a
counter at dispatch entry first, collect, and only then choose. That counter is
a separate telemetry change and needs its own review.

## Consequences

Good:

- The 10-tool and 40 KB limits fail a PR that breaks them, before data arrives.
- The refresh is one documented query, so anyone with monitoring access can
  finish this.

Bad:

- `core_router` gives no round-trip saving until the list lands.
- #8760 cannot compare `router` and `core_router` yet.

## Implementation plan

- Add `go/internal/mcp/core_tool_set_test.go` with the guardrail test and a
  seeded-violation test of the guard itself.
- The tests live in `go/internal/mcp` because `go/internal/mcp/router` must not
  import the MCP root (`TestRouterDoesNotImportMCPRoot`).
- When data lands, change only `coreToolNames` in `core.go` and this record's
  table, and delete `TestCoreToolNamesEmptyInScaffold` in the same PR.
- Operator signal for the chosen list: the same `eshu_dp_mcp_response_bytes`
  series, by `tool`, before and after the surface switch.

## Acceptance criteria

- [x] A test fails when `coreToolNames` names an unregistered tool or holds
      more than 10 entries.
- [x] A test reports the serialized size of the core set and fails above 40 KB.
- [x] This record includes the exact PromQL, so the list can be refreshed.
- [x] This record contains no deployment hostnames, account names, or employer
      identifiers.
- [ ] The query was run on a hosted deployment over at least 14 days and the
      shares are in the table above.
- [ ] The chosen list, at most 10 tools, is in `core.go`, with a reason per
      tool.

<details>
<summary>Evidence and measurements</summary>

Commands (run from `go/`):

```bash
go test ./internal/mcp/ -run 'TestCoreRouterToolSetGuardrails|TestCoreSetProblemsFlagsSeededViolations' -count=1 -v
```

The guardrail test logs `core_router core set: N tools, B serialized bytes`.
With the list empty it reports 0 tools and 2 bytes (`[]`).

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
