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
| `data` | 597,909 |
| envelope | 598,012 |
| escaped resource copy | 653,814 |
| counted (two copies) | 1,252,100 (477.6% of 262,144) |

The first cut of the fixture measured 543,644 B of data and 1,137,086 counted
(433.8%). Review found the fixture left out the lists `deployment_evidence`
builds from repository content when the graph holds no evidence
(`shared_config_paths`, `delivery_paths`, `deployment_artifacts`, and the
story lines; they have no row cap of their own), so the fixture now holds 100
of each list and a story sentence. The figures above include them.

The heaviest keys in `data`: `delivery_paths` 121,916; `deployment_evidence`
115,748; `instances` 57,301; `controller_overview` 41,241; `cloud_resources`
36,501; `deployment_facts` 36,410; `image_registry_truth` 29,401;
`k8s_resources` 20,071; `provisioned_platforms` 17,651; `story` 14,082;
`deployment_sources` 14,001; `topology_edges` 13,597.

## After: MCP default (`evidence_detail: handles`, no `sections`) at cap

| Source | Counted bytes |
| --- | --- |
| `TestApplySectionSelectionHandlesDefaultFitsBudget` (deployment package, two-copy estimate) | 198,538 (75.7%) |
| `TestTraceDeploymentChainDefaultArgumentsFitBudget` (real `dispatchToolWithOptions` and `estimateResponseBytes`, summary text included) | 198,758 (75.8%) |

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
| enrichment 50, overview carries hostname/entrypoint/api copies | 1,344,092 | 253,050 (96.5%) |
| 5 platforms per instance, enrichment 100 (`max_depth` >= 10) | 1,863,466 | 273,376 (104.3%) |

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

## Review fixes

Review of the first cut found four defects, each now covered by a test that
failed first:

- The drilldown arguments carried only `service_name`, `sections`, and
  `evidence_detail`. The MCP adapter defaults `direct_only` to true, so an
  agent that called with `direct_only: false` and followed the drilldown got a
  trace that never built `consumer_repositories`, and `section_detail`
  reported that family as full with total 0. `drilldown_arguments` now carries
  the request's `direct_only`, `max_depth` (as normalized), and
  `include_related_module_usage`
  (`TestApplySectionSelectionDrilldownReplaysTheOriginalRequest`,
  `TestTraceDeploymentChainDrilldownCarriesTheRequestArguments`).
- `sections: []` reached the handler as the mode's default set, but the MCP
  adapter treated a present list as a drilldown and set `evidence_detail` to
  `full`, which ships every family. The adapter now reads nil and an empty list
  as naming nothing (`TestRouteTraceDeploymentChainEvidenceDetailDefault`).
- The content-derived `deployment_evidence` lists were neither dropped under
  handles nor counted, and the fixture left them out. Handles mode now drops
  them and `section_detail.deployment_evidence.total` counts them
  (`TestApplySectionSelectionCountsAndDropsContentDerivedEvidenceLists`).
- The `image_registry_truth` handle lost the ambiguity qualifier; it now keeps
  `match_strength`.

Not changed: a non-string `evidence_detail` on MCP falls back to the default
instead of returning a 400, like every other typed argument read through
`Arguments.String`.

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
