# #7174 trace_deployment_chain size bound

`trace_deployment_chain` had no size bound by construction. Every family has
its own row cap, but the caps add up. With every family at its cap, the MCP
result was more than four times the dispatcher's 262,144-byte budget. The MCP
default now returns identity handle rows and omits the derived families.
`data.section_detail` and `truth.omissions` report each cut.

## What was measured

All numbers come from the committed tests and were run locally on this branch.
The fixture is `testutil.TraceAtCapWorkloadContext`. Every family is filled to
its documented cap: 50 instances, sources, cloud and k8s resources,
controllers, image refs, hostnames, entrypoints, network paths, and deployment
artifacts, plus 25 enrichment rows at the default `max_depth`.

The fixture is synthetic. Row widths are hand-built from the producer code
(`trace_deployment_sources.go`, `deployment/cloud_evidence.go`,
`deployment/gitops_helpers.go`, `repository.BuildGraphDeploymentEvidence`,
`oci` truth rows, and entity runtime topology), so treat any byte figure as
accurate to about +/-30%, not as a recording from a live graph.

"Counted" means the dispatcher's two-copy accounting: `structuredContent` plus
the escaped embedded resource. A result that only fits through the
resource-only fallback does not count as under budget.

## Before: full response at cap

Source: `go test ./internal/query/impact/deployment -run TestResponseAtCapSize -count=1 -v`.

| Measure | Bytes |
| --- | --- |
| `data` | 543,644 |
| envelope | 543,747 |
| escaped resource copy | 593,339 |
| counted (two copies) | 1,137,086 (433.8% of 262,144) |

The heaviest keys in `data`: `delivery_paths` 102,216; `deployment_evidence`
81,183; `instances` 57,301; `controller_overview` 41,241; `cloud_resources`
36,501; `deployment_facts` 36,410; `image_registry_truth` 29,401;
`k8s_resources` 20,071; `provisioned_platforms` 17,651; `story` 14,082;
`deployment_sources` 14,001; `topology_edges` 13,597.

## After: MCP default (`evidence_detail: handles`, no `sections`) at cap

| Source | Counted bytes |
| --- | --- |
| `TestApplySectionSelectionHandlesDefaultFitsBudget` (deployment package, two-copy estimate) | 198,336 (75.7%) |
| `TestTraceDeploymentChainDefaultArgumentsFitBudget` (real `dispatchToolWithOptions` and `estimateResponseBytes`, summary text included) | 198,556 (75.7%) |

Both tests assert a ceiling of 80% of the budget (209,715 bytes). The dispatch
test also asserts `!ResourceOnly`, `!IsError`, and that `truth.omissions`
survives the MCP envelope decode and lists `delivery_paths` as `omitted`.

The heaviest keys in the handles `data`: `story` 14,082; `deployment_evidence`
8,797; `controller_overview` 8,691; `image_registry_truth` 8,201; `instances`
8,001; `provisioned_platforms` 6,101; `cloud_resources` 5,901; `k8s_resources`
5,611; `deployment_sources` 5,351; `section_detail` 4,836.

The HTTP default stays `full`. `TestApplySectionSelectionFullIsByteIdenticalToToday`
checks both the sample dossier fixture and the at-cap fixture. Apart from the
added `evidence_detail` and `section_detail` keys, the full-mode response is
byte-identical to the response before this change, and `truth.omissions` is
absent.

## Residual: non-default worst cases (logged, not asserted)

Source: `TestApplySectionSelectionLogsNonDefaultWorstCase`.

| Scenario | Full counted | Handles counted |
| --- | --- | --- |
| enrichment 50, overview carries hostname/entrypoint/api copies | 1,229,352 | 252,848 (96.5%) |
| 5 platforms per instance, enrichment 100 (`max_depth` >= 10) | 1,748,726 | 273,174 (104.2%) |

The second row goes over the two-copy budget. This figure is an estimate, not a
measurement: the escaped resource copy is a little over half the counted
total, so this result should fit only through the dispatcher's resource-only
fallback. That fallback still returns the data and logs it, and
`eshu_dp_mcp_response_resource_fallback_total` counts it. The first row
overstates production. The production service-story overview copies
`api_surface` and emits only hostname and entrypoint counts
(`service/story_overview.go`), and the overviews handle projection removes the
`api_surface` endpoints.

## Handle key verification

Each handle key set was checked against its real producer. One key set differs
from the #7174 ruling. `provisioned_platforms` rows
(`entity/workload_provisioned_platforms.go` `normalizeProvisionedPlatform`)
have no `platform_source_id` or `relationship_type`, so their handle is
`{platform_id, platform_name, platform_kind}`. Deployment artifacts use `id`,
because `repository.BuildGraphDeploymentEvidence` maps `artifact_id` to `id`.

No-Regression Evidence (#7174): the change adds no graph or Postgres reads, and
no per-family query cap changes. `deployment.ApplySectionSelection` runs once,
after the response is built. It is O(rows): one pass over each emitted family
that copies each handle row into a new map. It never mutates the workload
context those rows are shared with. Counts, the story, overviews, and
`deployment_fact_summary` are still computed from the full lists before the
cut, following the #7169 emission-time pattern.

No-Observability-Change: the change adds no new metric, span, or log key. The
before and after proof is the existing
`eshu_dp_mcp_response_bytes{tool="trace_deployment_chain"}` histogram
(recorded by `recordResponseBytes` in `go/internal/mcp/dispatch_budget.go`),
together with the measurement test logs above. After this change, operators
should see that histogram's upper tail for this tool drop below the budget, and
`eshu_dp_mcp_response_over_budget_total` should stop counting it at default
arguments. Each response carries its own cut in `truth.omissions`.
