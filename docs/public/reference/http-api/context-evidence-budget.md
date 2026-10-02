# Context Evidence Budget

The workload and service context routes keep their payload inside the MCP
response budget by bounding the lists that grow with a service's evidence and
by offering a handle form of the rows. The rules are response shaping applied
after the reads; they never change what is stored or which rows are read.

| Route | MCP tool |
| --- | --- |
| `GET /api/v0/workloads/{workload_id}/context` | `get_workload_context` |
| `GET /api/v0/services/{service_name}/context` | `get_service_context` |

The story routes keep their own bounded dossier and are not covered here.

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
