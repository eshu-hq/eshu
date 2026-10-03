# #7129 workload and service context evidence and API surface bound

## Problem

After #7169 capped hostnames and entrypoints, `get_workload_context` and
`get_service_context` were still over the 256 KiB MCP response budget on
populated ops-qa services. The last ops-qa re-measure (issue comment of
2026-09-27, image `sha-1de13f4`) put two workloads with 72 and 78 endpoint
edges at 344,412 and 287,308 bytes (est2x, both wire copies counted), and named
`deployment_evidence.artifacts` and a byte-identical `api_surface.endpoints`
under `deployment_overview.api_surface` as the remaining bulk.

What the code does with those lists:

- `deployment_overview.api_surface.endpoints` is a second copy of the rows
  already shipped at the top-level `api_surface.endpoints`.
- `deployment_evidence.artifacts` comes from the Postgres read model when it has
  rows (`LoadRepositoryDeploymentEvidence`), capped at 50 in total across both
  directions. Only when the read model has no rows does the graph fallback run,
  and it reads 50 outgoing plus 50 incoming rows. Which path the two ops-qa
  workloads took is NOT_CHECKED.
- `delivery_paths`, `delivery_workflows` and `shared_config_paths`
  (content-derived) had no cap on the context routes.
- The content-fallback `buildServiceAPISurface` had no endpoint cap; only the
  graph path stopped at 50.
- Each artifact row carries about 1.4 KB of wire JSON when every column is
  populated, so 50 rows are about 72 KB of payload (about 144 KB est2x) before
  any other field.

## Change on the hot path

Two parts, both response shaping after the reads. No graph or Postgres query
changes.

Row caps. `WorkloadContextResultLimits` (`querycontract/context_limits.go`) now
calls `capContextBudgetRows` (`querycontract/context_limits_budget.go`) on the
workload and service context surfaces. It runs after every consumer of the full
lists has read them, so the cut changes only what ships. It cuts
`api_surface.endpoints` and the four `deployment_evidence` lists to
`ContextStoryItemLimit` (50) on a copy of the evidence map, drops the endpoint
rows from `deployment_overview.api_surface` (counts stay; `endpoints_shipped_at`
names the top-level list), and reports each cut in `partial_reasons`,
`result_limits.truncated`, `deployment_evidence.raw_limits`, and
`result_limits.artifact_count`. The same reasons now also report a list whose
read had already stopped at its own bound (`api_surface.detail_truncated`,
`deployment_evidence.artifacts_truncated`), which earlier responses never
surfaced. `artifact_count` counts rows read, so it is a floor when the read
itself stopped at its bound.

Row detail. `evidence_detail` (`full` or `handles`) is a new query parameter on
`GET /api/v0/workloads/{id}/context` and `GET /api/v0/services/{name}/context`
(`querycontract/context_evidence_detail.go`). `full` is the HTTP default, so the
console and existing callers see the same rows. `handles` projects artifact rows
to `id`, `relationship_type` and `resolved_id` (the keys the trace tool uses),
endpoint rows to `id`, `path` and `methods`, drops `evidence_index` (a regrouping
of the same artifacts) and the content-derived evidence values that have no row
cap of their own (`deployment_artifacts`, `delivery_family_paths`,
`delivery_family_story`, `topology_story`, `relationship_overview` and the
capped `delivery_paths`, `delivery_workflows`, `shared_config_paths`), the rule
`trace_deployment_chain` already applies, keeps every count, and lists each
reduced family with its total in `truth.omissions`. The MCP `get_workload_context` and `get_service_context`
tools default to `handles`; an explicit value wins and an unknown one is a 400.
This is the summary-plus-handle direction of the issue, on the same pattern as
#7174 for `trace_deployment_chain`. The arbiter ruled that a smaller page (25)
is refused because the console reads these rows, and that a compact projection
is a console contract change that needs its own measurement.

No-Regression Evidence:
- Metric: est2x, both MCP wire copies counted, against the 262,144-byte budget
  from `go/internal/mcp/dispatch_budget.go`.
- Fixture: `entity/workload_context_budget_test.go` and
  `entity/context_evidence_detail_test.go` drive the real handlers. The fixture
  combines the 671-hostname outlier (50 hostnames, 50 entrypoints and 50 network
  paths through the real enrichment path), 78 endpoint edges (50 rows read) and
  every artifact column populated. Fixture figures, not ops-qa figures:
  - graph fallback shape (100 artifact rows), caps only: 309,454 est2x;
  - read-model shape (50 artifact rows), `evidence_detail` full: 290,354 on the
    workload route and 290,352 on the service route, both over budget;
  - the same shape with `handles`: 114,314 and 114,312, 43.6% of the budget.
  The row caps alone do not fit this fixture, which is why `handles` is the MCP
  default. The handler fixture does not populate the content-derived evidence
  values, because they come from a content path it does not model. They are
  uncapped under `full`; under `handles` they are dropped, and
  `querycontract/context_evidence_detail_test.go` pins that with a context that
  holds each of them. So the `handles` size is bounded by the capped lists plus
  identity rows, not by an unmeasured family.
- Seeded violations: with the cap call disabled, the cap tests fail; with the
  `ApplyContextEvidenceDetail` call disabled on the workload route, the
  worst-case budget test fails; restored, they pass.
- Safety: copies of two or three map headers and one projection pass over at
  most 100 small rows after the reads. No query, round trip or lock.
- Backend: none. The tests use fake graph readers.
- A review comment pointed out that the budget guarantee did not cover those
  families; dropping them under `handles` is the fix, and the unit tests are
  the proof. No `full`-mode figure is claimed for a service heavy in them.
- NOT_CHECKED: an ops-qa re-measure of `get_workload_context` and
  `get_service_context`, including which artifact read path the two outlier
  workloads took. The host was unreachable from the development laptop on
  2026-10-02 (connection timeout). The fixture figures above are not a claim
  that either ops-qa workload now fits; the post-deploy re-measure is owed and
  #7129 stays open until it runs.

No-Observability-Change: no metric, span or log changes. The operator-visible
signal is in the response: `partial_reasons`, `result_limits.truncated` and
`artifact_count`, `deployment_evidence.raw_limits`, `evidence_detail`, and
`truth.omissions`. The existing MCP budget signals
(`eshu_dp_mcp_response_bytes`, `eshu_dp_mcp_response_over_budget_total`) show
whether either tool still crosses the budget.

## Follow-up: infrastructure rows (measured on ops-qa after #7520 deployed)

A scan of all 809 indexed repositories on build 4274e83 found 25 services still
over the MCP budget at default arguments. In all 25 the largest field is
`infrastructure` (169,807 to 585,055 bytes of 245,349 to 606,211 byte bodies,
read over the HTTP route). On one service it holds 1,161 rows of 69 / 139 / 295
bytes (min / median / max); the read bound is 5,000 rows and the 50-row context
caps did not cover the list. The change cuts it to 50 rows with the total on
`result_limits.infrastructure_count` and the reason `infrastructure_rows_truncated`.

No-Regression Evidence: unit tests on the cap and its within-limit and story-surface
cases; no query changes. The effect on the 25 services is NOT_CHECKED until this
build is deployed to ops-qa and they are re-measured.
No-Observability-Change: the signal is in the response (`partial_reasons`, `result_limits`).

## Follow-up: entrypoint_candidates rows (measured on ops-qa with #7528 merged)

The largest `get_service_context` response read over the HTTP route was
240,068 bytes against the 262,144 byte budget, with `result_limits.truncated`
true. By JSON size its payload fields were `entrypoint_candidates` 62,978
(357 rows, no cap), `deployment_overview` 42,620, `api_surface` 42,044,
`documentation_overview` 37,413, `entrypoints` 10,084 (50, capped), `hostnames`
8,184 (50, capped); `infrastructure` was 33 rows. `entrypoint_candidates` is
built from the repository's content evidence per request with no read bound,
so it grows with repository size, the same defect class as `infrastructure`.

The change cuts it to 50 rows after every consumer of the full list has run.
The total is on `result_limits.entrypoint_candidate_count`, read before the
cut, and the reason is `entrypoint_candidates_truncated`. The story surface
ships its own bounded copy and is not cut. Nothing downstream of the cap reads
the list, and the console does not read it.

By code reading only (not measured): `deployment_overview` carries counts and
a copy of `api_surface` minus the endpoint rows, but `api_surface` also holds
`docs_routes`, `hostnames`, `spec_paths`, `spec_versions` and `api_versions`
with no cap, and `documentation_overview` carries `api_spec_paths`, also
uncapped. Those are the likely source of the remaining bytes and need their
own measurement before a cap.

No-Regression Evidence: unit tests on the cut, the within-limit case, the story
surface and the source slice; no query changes. With the cap call disabled the
cut test fails (`entrypoint_candidates len = 357, want 50`). The effect on the
largest service is NOT_CHECKED until this build is deployed to ops-qa and it is
re-measured.
No-Observability-Change: the signal is in the response (`partial_reasons`, `result_limits`).
