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
  `evidence_detail_drilldown`, keeps every count, and lists each reduced family
  in `truth.omissions` with the rows it held: `detail` is `handles` for
  projected rows and `omitted` for a dropped value. A list the row cap had
  already cut reports its pre-cut count.
- The MCP `get_workload_context` and `get_service_context` tools default to
  `handles`; an explicit `evidence_detail` wins.

## Reaching the rows

- Under `full`, `deployment_evidence.evidence_index.*.resolved_ids` names every
  artifact row read, and `get_relationship_evidence` returns one by
  `resolved_id`.
- Under `handles` the index is dropped, so reaching an artifact row beyond the
  50 shipped, or any dropped value, takes a call with `evidence_detail: full`.

## Service investigation repository count

`GET /api/v0/investigations/services/{service_name}` cuts `repositories_considered`
to 50 rows and closes a cut list with one `__truncated__` marker row.
`coverage_summary.repository_count` is the true number of repositories before
that cut, and it never counts the marker. `coverage_summary.repositories_truncated`
is `true` when the list was cut, so a caller reads one field instead of scanning
rows for the marker: 120 repositories report `120` and `true`, exactly 50 report
`50` and `false`, and 51 report `51` and `true`.
`repositories_with_evidence_count` counts only the rows returned, so it can be
lower than `repository_count` when the list was cut. `coverage_summary.truncated`
stays the combined flag and is also `true` when `repositories_truncated` is.

## Service story

`GET /api/v0/services/{service_name}/story` and the MCP `get_service_story` tool
take the same `evidence_detail` parameter, `full` or `handles`. An unknown value
returns 400 `invalid_argument` before any read runs. The shaping runs on the
built dossier, so no query is added and every count and every top-level status,
truth, and freshness field is computed from the full rows.

- `full` is the HTTP default and returns the dossier as before, with one change:
  the endpoint rows ship once, at the top-level `api_surface.endpoints`.
  `deployment_overview.api_surface` keeps its counts and
  `endpoints_shipped_at` points at the top-level rows (#8798).
- `handles` is the MCP default. A large service's full dossier measured 266 KB
  and, because the MCP result carries the payload twice, failed the response
  budget with `mcp_response_over_budget`. Under `handles` the dossier keeps
  every section and projects the bulky lists to identity rows:
  - `deployment_evidence.artifacts` and `api_surface.endpoints`, as on the
    context routes, with the content-derived `deployment_evidence` values
    dropped.
  - `code_to_runtime_trace.segments[].evidence`, to the identity keys of that
    segment. `service_identity`, `ci_cd`, `image_package`, and `runtime` hold the
    only copy of their rows, so they keep every identity key: the
    `image_package` rows keep the image reference, digest, and SBOM attachment
    and document identity and status, and a `ci_cd` row that wraps the
    top-level `ci_cd_evidence` summary becomes
    `evidence_summary_shipped_at: ci_cd_evidence`. A segment
    whose rows repeat another section (`code_entrypoints`, `deployment_config`,
    `cloud_dependencies`) keeps a preview of 10 rows and sets `truncated`,
    `evidence_shipped`, and `evidence_omitted`; `evidence_count` stays the total.
  - `upstream_dependencies`, `hostnames`, `entrypoints`, `dependents`,
    `consumer_repositories`, `provisioning_source_chains`, `cloud_resources`,
    `uncorrelated_cloud_resources`, and the `investigation` repositories, to
    identity keys. Each list keeps its length. The identity keys include the
    row-level truth labels: `confidence` on upstream dependencies and chains;
    `candidate_status`, `missing_relationship`, `match_basis`,
    `service_anchor_status`, and `service_anchor_reason` on uncorrelated cloud
    resources; `relationship_basis` on cloud resources. The investigation
    truncation marker row keeps its `note`.
  - `evidence_graph.edges`, to `id`, `source`, `target`, `relationship_type`,
    `confidence`, and `resolved_id`. `evidence_graph.nodes` keeps the nodes a shipped edge touches
    and the workload and its source repository; `node_count`, `nodes_truncated`,
    and `nodes_omitted` report the cut.
  - `downstream_consumers.graph_dependents` and `content_consumers` are dropped
    because the same rows ship in `dependents` and `consumer_repositories`;
    `graph_dependents_shipped_at` and `content_consumers_shipped_at` name them.
- Under `handles` the response sets `evidence_detail` and
  `evidence_detail_drilldown`, and lists each reduced family in `truth.omissions`
  with its true total, the count the story recorded before it capped the list
  (`detail` is `handles` for projected rows and `omitted` for a dropped list).
  The investigation packet records no count past its 50-row cap, so its total is
  a lower bound, the 50 rows it shipped; the `__truncated__` marker row says
  more exist. `truth.level`,
  `truth.freshness`, and `answer_metadata` are unchanged. `answer_packet.truth`
  copies the truth envelope, so it carries the same `omissions`.
- To reach the rows under `handles`, repeat the call with `evidence_detail: full`,
  or fetch one relationship by `resolved_id` with `get_relationship_evidence`.

Measured on a synthetic large-service fixture with every list past its 50-row
cap, the dossier is 334,467 bytes under `full` and 110,243 bytes under `handles`
(33.0 percent). The MCP result carries the envelope twice, once structured and
once as an escaped JSON string; that dual result is 717,986 bytes under `full`,
over the 262,144-byte budget, and 252,676 bytes under `handles`, inside it.
A story with no attached cloud resources and 60 uncorrelated cloud-resource
candidates (the enrichment loads candidates only when no cloud resource
attached, so the two lists never ship together) is 120,727 bytes of data under
`handles` and 274,786 bytes in the dual shape. That exceeds the budget, so
dispatch falls back to the resource copy alone: 130,467 bytes as the structured
copy, 144,319 as the escaped text copy. Sending the result once (#8800)
removes the fallback.

### Service story evidence

No-Regression Evidence: the service story change reads the dossier the builder
already produced and adds no graph, Postgres, or content read; the shaping runs
in the handler after `BuildServiceStoryEnvelope` returns, and the in-process
service intelligence report calls the builder directly and is not shaped. The
only builder change drops the endpoint rows from the overview's copy of
`api_surface`, which removes about 12 KB of serialization work on this fixture.
Input shape: a fixture with every row list past its 50-row cap (60 rows each),
plus CI/CD and supply-chain evidence. Under `full`: 334,467 bytes. Under
`handles`: 110,243 bytes (33.0 percent of `full`); the escaped dual MCP result
is 252,676 bytes against the unchanged 262,144-byte budget (`full`: 717,986).
`go test ./internal/query/service -run 'OversizedServiceStory|StoryHandles'
-count=1` pins the budget on the escaped size, the unchanged truth and count
fields, a marker on every cut, no empty handle row, the true omission total of
every reduced family, and the per-row truth labels.

Observability Evidence: `eshu_dp_mcp_response_bytes` and
`eshu_dp_mcp_response_over_budget_total`, both labeled by tool, show the
`get_service_story` size falling under the budget and the over-budget count
stopping. Each response names its level in `evidence_detail` and every reduced
family, with its total, in `truth.omissions`. No metric, span, or log field is
added.
