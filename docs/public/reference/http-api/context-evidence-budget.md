# Context Evidence Budget

The workload and service context routes keep their payload inside the MCP
response budget by bounding the lists that grow with a service's evidence and
by offering a handle form of the rows. The rules are response shaping applied
after the reads; they never change what is stored or which rows are read.

| Route | MCP tool |
| --- | --- |
| `GET /api/v0/workloads/{workload_id}/context` | `get_workload_context` |
| `GET /api/v0/services/{service_name}/context` | `get_service_context` |

The service story route has its own section at the end of this page. The
workload story route keeps its own bounded dossier and is not covered here.

## Row caps

`api_surface.endpoints` and the `deployment_evidence` lists `artifacts`,
`delivery_paths`, `delivery_workflows`, and `shared_config_paths` are each cut to
50 rows.

- Each cut is named in `partial_reasons`: `api_surface_endpoints_truncated`,
  `deployment_evidence_artifacts_truncated`,
  `deployment_evidence_delivery_paths_truncated`,
  `deployment_evidence_delivery_workflows_truncated`, and
  `deployment_evidence_shared_config_paths_truncated`. Any of them sets
  `result_limits.truncated`.
- `deployment_evidence.raw_limits` reports the pre-cut count of each cut list.
- A read that stopped at its own bound (`api_surface.detail_truncated`,
  `deployment_evidence.artifacts_truncated`) is reported by the same reasons;
  earlier responses did not disclose it.
- `api_surface.endpoint_count` is the endpoint total. `artifact_count` in
  `deployment_evidence` and in `result_limits` counts the artifact rows read, so
  it is a floor when the read itself stopped at its bound.
- No route returns the cut rows of the lists other than `artifacts`.
- `infrastructure` is cut to 50 rows. `result_limits.infrastructure_count` is the
  total read, and the cut is named `infrastructure_rows_truncated`. The read
  itself stops at 5,000 rows and reports `infrastructure_truncated`; before this
  cap a service could ship all 5,000 rows (about 730 KB).
- `entrypoint_candidates` is cut to 50 rows. `result_limits.entrypoint_candidate_count`
  is the total before the cut, and the cut is named `entrypoint_candidates_truncated`.
  The list had no bound and grows with repository size (357 rows, about 63 KB, on the
  largest service measured).
- `deployment_overview.api_surface` keeps its counts but not the endpoint rows;
  `endpoints_shipped_at` points at the top-level `api_surface.endpoints`.

## Evidence detail

`evidence_detail` is a query parameter, `full` or `handles`. Any other value
returns 400 `invalid_argument` before any read runs.

- `full` is the HTTP default and returns every row that survives the caps.
- `handles` projects `deployment_evidence.artifacts` rows to `id`,
  `relationship_type`, and `resolved_id`, and `api_surface.endpoints` rows to
  `id`, `path`, and `methods`. It drops `deployment_evidence.evidence_index`
  (a regrouping of the same artifacts) and the values built from repository
  content, which have no row cap of their own: `deployment_artifacts`,
  `shared_config_paths`, `delivery_paths`, `delivery_family_paths`,
  `delivery_family_story`, `delivery_workflows`, `topology_story`, and
  `relationship_overview`. A value that holds no rows stays.
- Under `handles` the response sets `evidence_detail` and
  `evidence_detail_drilldown`, and lists each reduced family in `truth.omissions`
  with its total (`detail` is `handles` for projected rows and `omitted` for a
  dropped list). Each total is the story's own pre-cap count and may be a lower
  bound, because the story carries a flag for only some of the bounds its reads
  apply; treat a total as exact only for the families under "Exact totals" below.
  `truth.level`, `truth.freshness`, and `answer_metadata`
  are unchanged. `answer_packet.truth` copies the truth envelope, so it carries
  the same `omissions`.
- To reach the rows under `handles`, repeat the call with `evidence_detail: full`,
  or fetch one relationship by `resolved_id` with `get_relationship_evidence`.

### Omission total rules

Every total is the story's pre-cap count and may be a lower bound: the story
carries a flag for only some of the bounds its reads apply. Treat a total as
exact only for the two entries under "Exact totals". For every other family the
"Lower bound when" column names the known flags and limits and is not
exhaustive, so a total can be low with none of them set.

Exact totals, proven on every path that fills them:

- `code_to_runtime_trace.service_identity.evidence`: the single identity row,
  built from the workload and repository ids with no read behind it.
- `api_surface.endpoints` when `api_surface.truth_basis` is `graph`: the total is
  `endpoint_count`, `count(endpoint)` from the graph, which has no `LIMIT` (only
  the row query does). Any other `api_surface` is the content fallback and a lower
  bound.

The flags the table names:

- A deployment artifact bound is `deployment_evidence.artifacts_truncated`, or
  one of the limitations `deployment_artifacts_candidates_budget_reached`,
  `deployment_artifacts_related_file_list_truncated_at_5000`, and
  `deployment_artifacts_anchor_file_list_truncated_at_5000`.
- `dependents_truncated`, `consumer_repositories_truncated`, and
  `provisioning_source_chains_truncated` come from the bounded downstream read
  (`result_limits.downstream_read_limit`). `consumer_repositories_truncated`
  is also set when the service evidence file read hit
  `result_limits.evidence_file_read_limit`.
- A read of 50 rows with no flag of its own is the `cloud_resources` read, the
  runtime instance read, and the framework route read (50 files): their totals
  cannot say more exist. The framework route read has no flag for the 50-file
  bound; the row cap can still set `api_surface.truncated`, but nothing names
  the file read or says the total is low.
- `infrastructure_truncated` belongs to the infrastructure panel, which has no
  omission section, and does not bound the content-built delivery lists.

| `truth.omissions` section | Count the total reads | Lower bound when |
| --- | --- | --- |
| `deployment_evidence.artifacts` | `deployment_evidence.artifact_count`, the artifact rows read | a deployment artifact bound |
| `deployment_evidence.deployment_artifacts`, `shared_config_paths`, `delivery_paths`, `delivery_family_paths`, `delivery_family_story`, `delivery_workflows`, `topology_story`, `relationship_overview` | rows held, or the `raw_limits` count when the story cut the list | a deployment artifact bound |
| `api_surface.endpoints` | `api_surface.endpoint_count`: the graph count query (exact, see above), or, on the content fallback, the endpoint total of the specs found in the first 5,000 files; framework routes are not in that count | content fallback: `consumer_repositories_truncated`, and the framework route read (50 files, no flag for that bound), so 0 specs and 120 framework routes report 50; `api_surface.truncated` is set by the row cap, but it does not name the file read or say the total is low |
| `upstream_dependencies` | `result_limits.upstream_count`: artifact, dependency, and provisioning chain rows | a deployment artifact bound, or `provisioning_source_chains_truncated` |
| `dependents`, `downstream_consumers.graph_dependents` | rows read | `dependents_truncated` |
| `consumer_repositories`, `downstream_consumers.content_consumers` | rows read | `consumer_repositories_truncated` |
| `provisioning_source_chains` | rows read | `provisioning_source_chains_truncated` |
| `hostnames`, `entrypoints`, `entrypoint_candidates`, `network_paths` | rows the service evidence file read produced | `consumer_repositories_truncated` |
| `cloud_resources` | rows read, at most 50 | the read stops at 50 rows with no flag of its own |
| `uncorrelated_cloud_resources` | rows read, at most 50 | `uncorrelated_cloud_resources_truncated` |
| `code_to_runtime_trace.service_identity.evidence` | the single identity row | exact, see above |
| `code_to_runtime_trace.code_entrypoints.evidence` | the endpoint rows the `api_surface` read returned plus entrypoint rows, not `endpoint_count`: graph rows stop at `api_surface.detail_limit`, content-fallback rows include the framework routes | `api_surface.detail_truncated` or `consumer_repositories_truncated`, and on the content fallback the framework route read (50 files, no flag for that bound) |
| `code_to_runtime_trace.ci_cd.evidence` | one `ci_cd_evidence` summary row (a count of 1 whatever the summary holds), or the `delivery_workflows` rows | a deployment artifact bound, for the `delivery_workflows` rows only |
| `code_to_runtime_trace.image_package.evidence` | SBOM attachment rows for at most 50 image references or, with no supply-chain reader, the image and package refs in the deployment evidence rows | `image_refs_truncated` on the segment, or a deployment artifact bound |
| `code_to_runtime_trace.deployment_config.evidence` | `artifacts`, `delivery_paths`, and `shared_config_paths` rows | a deployment artifact bound |
| `code_to_runtime_trace.runtime.evidence` | runtime instances read, at most 50 | the read stops at 50 rows with no flag of its own |
| `code_to_runtime_trace.cloud_dependencies.evidence` | the `cloud_resources` rows, or the uncorrelated candidates when none attached | as `cloud_resources`, or as `uncorrelated_cloud_resources` |
| `evidence_graph.edges` | `evidence_graph.edge_count`: artifact edges plus one edge per instance | a deployment artifact bound, or 50 instances read |
| `evidence_graph.nodes` | nodes built from artifacts, instances, consumer repositories, and dependents | a deployment artifact bound, `dependents_truncated`, `consumer_repositories_truncated`, or 50 instances read |
| `investigation.repositories_considered` | `investigation.coverage_summary.repository_count`, the repository set built from the artifacts, dependents, consumer repositories, and provisioning chains the story holds, taken before its 50-row cap; never counts the marker row | a deployment artifact bound, `dependents_truncated`, `consumer_repositories_truncated`, or `provisioning_source_chains_truncated` |
| `investigation.repositories_with_evidence` | the non-marker rows it shipped, at most 50 (the packet records no count past the cap) | the `investigation.repositories_considered` conditions, or `repositories_considered` ends with the `__truncated__` marker row |

Measured on a synthetic large-service fixture that holds 60 rows in every list
and the row keys the production builders emit, the story ships 50 rows per list
(the cap; `TestOversizedServiceStoryShipsFiftyOfSixtyRows` counts them), so the
measured bytes are the 50-row ship size of a story with every list past its
cap, not a bound above it. The 60 only drives the pre-cap totals in
`raw_context_limits`. The dossier is 360,361 bytes
under `full` and 128,547 bytes under `handles` (35.7 percent). The MCP result
carries the envelope twice, once structured and once as an escaped JSON string.
Under `full` even one copy is over the 262,144-byte budget (366,409 bytes
structured, 405,267 escaped), so the resource-copy fallback cannot help and the
dispatch returns `mcp_response_over_budget`, the #8798 failure. Under `handles`
the single copy fits the budget (138,263 bytes structured, 152,707 escaped). The
MCP result still carries the payload twice (290,970 bytes), so the dispatch
answers it as the resource copy alone and records
`eshu_dp_mcp_response_resource_fallback_total` until #8800 sends the result
once. A story with no attached cloud resources and 60 uncorrelated
cloud-resource candidates (the enrichment loads candidates only when no cloud
resource attached, so the two lists never ship together; the fixture holds 60
and the story ships 50) is 366,502 bytes under `full`, 125,578 under `handles`,
and 284,854 in the dual shape, and also falls back: 135,320 bytes as the
structured copy, 149,534 as the escaped text copy.

The standard fixture leaves out `entrypoint_candidates` and `network_paths`,
which also cap at 50 rows. With 60 rows of each added (the fixture holds 60 of
every family and the story ships 50), the story is 385,793 bytes under `full` and 146,879 under `handles`
(`entrypoint_candidates` 10,837 to 7,487 bytes, `network_paths` 14,301 to
10,551). That shape is 330,200 bytes in the dual MCP shape, so dispatch falls
back to the resource copy alone (156,843 bytes structured, 173,357 escaped).
`TestOversizedServiceStoryHandlesSingleCopyFitsMCPBudget`,
`TestCandidateHeavyServiceStoryHandlesMCPSize`, and
`TestEntrypointFamiliesStoryHandlesMCPSize` gate the escaped single copy of
each shape and log these figures. `TestServiceStoryHandlesAnsweredAsOneCopy` in
`go/internal/mcp` drives the standard fixture through the dispatch: under
`handles` it is answered as the resource copy and counted as a fallback, and
under `full` it returns `mcp_response_over_budget`.

### Service story evidence

No-Regression Evidence: the service story change reads the dossier the builder
already produced and adds no graph, Postgres, or content read; the shaping runs
in the handler after `BuildServiceStoryEnvelope` returns, and the in-process
service intelligence report calls the builder directly and is not shaped. The
only builder change drops the endpoint rows from the overview's copy of
`api_surface`, which removes about 12 KB of serialization work on this fixture.
Input shape: a fixture that holds 60 rows in every row list, past the 50-row
cap, so the story ships 50 per list, with the production row keys, plus CI/CD and supply-chain evidence. Under `full`:
360,361 bytes. Under `handles`: 128,547 bytes (35.7 percent of `full`); the
escaped single copy is 152,707 bytes against the unchanged 262,144-byte budget
(`full`: 405,267), and the dual result, 290,970 bytes (`full`: 771,676), takes
the resource-copy fallback until #8800.
`go test ./internal/query/service -run 'OversizedServiceStory|StoryHandles'
-count=1` pins the budget on the escaped size, the unchanged truth and count
fields, the preview and node-cut markers and the ship-once pointers, no empty handle row, the omission total of
every reduced family (the story's own pre-cap count), and the per-row truth labels.
`TestStoryHandlesKeepProductionRowTruthLabels` builds the rows with the
production builders and fails on a row key with no kept or dropped decision.

Observability Evidence: `eshu_dp_mcp_response_over_budget_total` stops rising
for `get_service_story` and `eshu_dp_mcp_response_resource_fallback_total`
counts the stories answered as one copy; both are labeled by tool, and
`eshu_dp_mcp_response_bytes` records each response size. Each response names its
level in `evidence_detail` and every reduced family, with its total, in
`truth.omissions`. No metric, span, or log field is added.
