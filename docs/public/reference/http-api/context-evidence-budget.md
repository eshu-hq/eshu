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
  - `upstream_dependencies`, `hostnames`, `entrypoints`, `entrypoint_candidates`,
    `network_paths`, `dependents`, `consumer_repositories`,
    `provisioning_source_chains`, `cloud_resources`,
    `uncorrelated_cloud_resources`, and the `investigation` repositories, to
    identity keys plus the row truth labels, the fields that say how a row was
    established. Each list keeps its length, and the provenance detail (paths,
    reasons, matched values, source record ids) ships only under `full`. The
    kept keys follow the rows the production builders emit:
    - `upstream_dependencies`: `resolved_id`, `relationship_type`, `source`,
      `source_repo_id`, `target`, `target_repo_id`, `target_id`, and
      `confidence`.
    - `dependents`: `repo_id`, `repository`, and `relationship_types`.
    - `consumer_repositories`: `repo_id`, `repository`, `consumer_kinds`,
      `graph_relationship_types`, and `evidence_kinds`.
    - `provisioning_source_chains`: `repo_id`, `repository`,
      `relationship_types`, and `evidence_kinds`, which already include the
      relationship reasons.
    - `hostnames`: `hostname` and `environment`. `entrypoints`: `type`,
      `target`, `environment`, and `visibility`.
    - `entrypoint_candidates`: `candidate`, `classification`, and `reason`, the
      only statement of why the token is not an entrypoint.
    - `network_paths`: `path_type`, `from_type`, `from`, `to`, `platform_kind`,
      `environment`, and `visibility`; the constant `to_type` and the repeated
      `reason` go.
    - `cloud_resources`: `id`, `name`, `kind`, `resource_type`,
      `relationship_basis`, `resolution_mode`, `evidence_source`,
      `service_anchor_source`, and `service_anchor_reason`.
    - `uncorrelated_cloud_resources`: `id`, `name`, `kind`, `resource_type`,
      `candidate_status`, `missing_relationship`, `service_anchor_status`, and
      `service_anchor_reason`.

    The investigation truncation marker row keeps its `note`.
  - `evidence_graph.edges`, to `id`, `source`, `target`, `relationship_type`,
    `confidence`, and `resolved_id`. `evidence_graph.nodes` keeps the nodes a shipped edge touches
    and the workload and its source repository; `node_count`, `nodes_truncated`,
    and `nodes_omitted` report the cut.
  - `downstream_consumers.graph_dependents` and `content_consumers` are dropped
    because the same rows ship in `dependents` and `consumer_repositories`;
    `graph_dependents_shipped_at` and `content_consumers_shipped_at` name them.
- Under `handles` the response sets `evidence_detail` and
  `evidence_detail_drilldown`, and lists each reduced family in `truth.omissions`
  with its total, the count the story held before its own 50-row cap (`detail`
  is `handles` for projected rows and `omitted` for a dropped list). The total
  is exact only when the read behind the family was not itself cut, because
  several reads are bounded before the story sees their rows:
  - `dependents`, `consumer_repositories`, and `provisioning_source_chains`
    stop at `result_limits.downstream_read_limit` and set `dependents_truncated`,
    `consumer_repositories_truncated`, and
    `provisioning_source_chains_truncated`. Their totals are lower bounds when
    the matching flag is set.
  - `uncorrelated_cloud_resources` reads one row past 50 and sets
    `uncorrelated_cloud_resources_truncated`. Its total is a lower bound when
    that flag is set.
  - `cloud_resources` is read at most 50 rows with no truncation flag, so its
    total never passes 50 and cannot tell you more exist.
  - `upstream_dependencies` is built from deployment artifacts, dependencies,
    and the provisioning chains, so it is a lower bound when
    `provisioning_source_chains_truncated` or a deployment artifact bound is
    set.
  - `hostnames`, `entrypoints`, `entrypoint_candidates`, and `network_paths`
    come from the service evidence file read, so they are lower bounds when that
    read hit the `result_limits.evidence_file_read_limit` file bound (the story
    then sets `consumer_repositories_truncated`).

  `investigation.repositories_considered` reports the packet's repository
  count, `investigation.coverage_summary.repository_count`, which is taken
  before the 50-row cap and never counts the marker row. The packet records no
  such count for `investigation.repositories_with_evidence`
  (`repositories_with_evidence_count` counts only the rows returned), so that
  total is the rows it shipped, at most 50 and often fewer. It never ships the
  marker row. It is a lower bound when `repositories_considered` ends with the
  `__truncated__` marker row, and exact otherwise. `truth.level`,
  `truth.freshness`, and `answer_metadata` are unchanged. `answer_packet.truth`
  copies the truth envelope, so it carries the same `omissions`.
- To reach the rows under `handles`, repeat the call with `evidence_detail: full`,
  or fetch one relationship by `resolved_id` with `get_relationship_evidence`.

Measured on a synthetic large-service fixture with every list past its 50-row
cap and the row keys the production builders emit, the dossier is 360,361 bytes
under `full` and 128,547 bytes under `handles` (35.7 percent). The MCP result
carries the envelope twice, once structured and once as an escaped JSON string.
Under `full` even one copy is over the 262,144-byte budget (366,409 bytes
structured, 405,267 escaped), so the resource-copy fallback cannot help and the
dispatch returns `mcp_response_over_budget`, the #8798 failure. Under `handles`
the single copy fits the budget (138,263 bytes structured, 152,707 escaped). The
MCP result still carries the payload twice (290,970 bytes), so the dispatch
answers it as the resource copy alone and records
`eshu_dp_mcp_response_resource_fallback_total` until #8800 sends the result
once. A story with no attached cloud resources and
60 uncorrelated cloud-resource candidates (the enrichment loads candidates only
when no cloud resource attached, so the two lists never ship together; the 60
rows are a conservative synthetic size, because the production read returns at
most 50) is
366,502 bytes under `full`, 125,578 under `handles`, and 284,854 in the dual
shape, and
also falls back: 135,320 bytes as the structured copy, 149,534 as the escaped
text copy.

The standard fixture leaves out `entrypoint_candidates` and `network_paths`,
which also cap at 50 rows. With 60 rows of each added (every family past its cap;
the 60 is a conservative synthetic size, and production ships at most 50 of
any family), the story is 385,793 bytes under `full` and 146,879 under `handles`
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
Input shape: a fixture with every row list past its 50-row cap (60 rows each,
a conservative synthetic size; production ships at most 50 rows per list)
and the production row keys, plus CI/CD and supply-chain evidence. Under `full`:
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
