# HTTP IaC, Content, And Infra Routes

Use these routes for IaC cleanup candidates, AWS runtime drift, source/content
reads, shared-infrastructure tracing, impact analysis, and environment
comparison. The route list is verified against `go/internal/query`.

## Route Map

| Area | Routes |
| --- | --- |
| IaC inventory | `GET /api/v0/iac/resources` |
| IaC quality | `POST /api/v0/iac/dead` |
| AWS management and drift | `POST /api/v0/iac/unmanaged-resources`, `POST /api/v0/iac/management-status`, `POST /api/v0/iac/management-status/explain`, `POST /api/v0/iac/terraform-import-plan/candidates`, `POST /api/v0/aws/runtime-drift/findings` |
| Replatforming rollups | `POST /api/v0/replatforming/rollups` |
| Content | `POST /api/v0/content/files/read`, `POST /api/v0/content/files/lines`, `POST /api/v0/content/entities/read`, `POST /api/v0/content/files/search`, `POST /api/v0/content/entities/search` |
| Infrastructure | `POST /api/v0/infra/resources/search`, `POST /api/v0/infra/relationships`, `GET /api/v0/ecosystem/overview`, `GET /api/v0/cloud/resources` |
| Impact | `POST /api/v0/impact/trace-resource-to-code`, `POST /api/v0/impact/explain-dependency-path`, `POST /api/v0/impact/blast-radius`, `POST /api/v0/impact/change-surface`, `POST /api/v0/impact/change-surface/investigate`, `POST /api/v0/impact/entity-map`, `POST /api/v0/impact/resource-investigation`, `POST /api/v0/compare/environments` |

OpenAPI remains canonical for full request and response schemas.

## IaC Inventory

`GET /api/v0/iac/resources` is a bounded, enveloped browse over the
authoritative Terraform/IaC graph projection. It backs the Console IaC page and
any client that needs a stable list of Terraform resources, modules, or data
sources.

- `kind` selects the node label: `resource` (default, `TerraformResource`),
  `module` (`TerraformModule`), or `data-source` (`TerraformDataSource`).
- `type` filters by Terraform resource type (e.g. `aws_iam_role`); for
  `data-source` it filters the data type. `provider` filters by provider (e.g.
  `aws`); provider is present only on canonical-sourced nodes, so a provider
  filter narrows to canonically attributed rows.
- `module` filters by module name. For resources and data sources it matches the
  `module."<name>".` address prefix; for modules it matches the module name
  exactly.
- `limit` is 1-200 and defaults to 50. The list is keyset-paginated and ordered
  by `(name, id)`; when `truncated` is true, pass `next_cursor.after_name` and
  `next_cursor.after_id` back as `after_name` and `after_id` to fetch the next
  page.

The endpoint requires the local-authoritative profile or higher; lower profiles
receive `501 unsupported_capability`. When the graph backend is not wired the
route returns `503`.

### IaC inventory performance and observability

Performance Evidence: `MATCH (n:TerraformResource) ... ORDER BY n.name, n.id
LIMIT $limit` against NornicDB (Compose `bolt://localhost:7687`, database
`nornic`) over 2,089 `TerraformResource` nodes. The `resource_type` and
`provider` filters use the `tf_resource_type` and `tf_resource_provider`
indexes; the `module` filter is a bounded `STARTS WITH` prefix on `n.name`
covering both the bare `module.<name>.` and for_each `module.<name>[` address
forms. Live smoke against a locally built `eshu-api` (production profile)
pointed at the running stack returned `200` for
`GET /api/v0/iac/resources?limit=5` in ~0.01-0.02s end to end (cold), ~1ms
warm; `limit=200` returned in ~0.11s; `kind=module`, `kind=data-source`, and
`module=` for_each filters each returned `200` in under 0.05s. The bounded
`limit+1` page keeps the read off the unbounded label scan path. No graph
schema or write path changed, so this is a new bounded read with no regression
to existing hot paths.

Observability Evidence: `eshu_dp_iac_resource_list_duration_seconds`
(histogram, `iac.kind` label) and `eshu_dp_iac_resource_list_errors_total`
(counter, `iac.kind` + `reason` labels) expose handler latency and failure
class; the `query.iac_resources` span carries the stable `http.route` and
`eshu.capability` attributes.

## IaC Cleanup

`POST /api/v0/iac/dead` requires `repo_id` or `repo_ids`. When reducer
reachability rows exist, the response uses materialized reachability. Otherwise
it falls back to bounded content analysis over Terraform, Helm, Kustomize,
Ansible, and Docker Compose artifacts.

`limit` defaults to 100 and is capped at 500. Dynamic or variable-selected
references are returned as ambiguous only when `include_ambiguous=true`.

## AWS Management And Drift

AWS management routes read active reducer materialization. They do not mutate
cloud resources, run Terraform, or write Terraform state.

`/iac/unmanaged-resources` and `/aws/runtime-drift/findings` require
`scope_id` or `account_id`; `region`, `finding_kinds`, `limit`, and `offset`
narrow the page. `limit` defaults to 100 and is capped at 500.

`/iac/management-status` and `/iac/management-status/explain` inspect one
resource. They require `scope_id` or `account_id` plus `arn` or `resource_id`.
For AWS, `resource_id` is an alias for the full ARN.

`/iac/terraform-import-plan/candidates` returns import blocks only for
safety-approved `cloud_only` findings in supported families. Ambiguous,
unknown, stale, state-only, security-review, and unsupported findings are
returned as refused candidates.

Supported families map an AWS finding to a Terraform resource type and a
deterministic import ID derived only from the finding ARN/`resource_id`:

| AWS family | Terraform type | Import ID |
| --- | --- | --- |
| `s3` | `aws_s3_bucket` | bucket name |
| `lambda` | `aws_lambda_function` | function name |
| `sns` | `aws_sns_topic` | topic ARN |
| `dynamodb` | `aws_dynamodb_table` | table name |
| `ecr` | `aws_ecr_repository` | repository name |
| `logs` | `aws_cloudwatch_log_group` | log group name |

A family is only mapped when its import ID is exact and non-secret. Findings
whose identity is ambiguous (for example SNS subscriptions, DynamoDB indexes,
and CloudWatch log streams) are refused with `missing_provider_import_id`
rather than guessed. Families that need a data-plane read, secret value, broad
policy synthesis, or an unsafe provider identity — including SQS queue URLs,
IAM, and KMS — stay refused: IAM and KMS are routed to `security_review_required`
by the safety gate, and unmapped families are refused with
`unsupported_resource_type`.

Management status values are:

- `managed_by_terraform`
- `terraform_state_only`
- `terraform_config_only`
- `cloud_only`
- `managed_by_other_iac`
- `ambiguous_management`
- `unknown_management`
- `stale_iac_candidate`

Raw tag values that look credential-like are redacted as `[REDACTED]`.

## Replatforming Rollups

`POST /api/v0/replatforming/rollups` aggregates the same active reducer drift
and IaC findings into bounded rollups by account, environment, and service. It
answers "how much of this account, service, or environment is declared,
applied, observed, unmanaged, stale, ambiguous, or blocked?" without paging
every row-level finding. It requires `scope_id` or `account_id` and the
local-authoritative profile or higher; lower profiles receive
`501 unsupported_capability`. `region`, `finding_kinds`, `limit`, and `offset`
narrow the bounded page; `limit` defaults to 100 and is capped at 500.

The response groups findings under `dimensions.account`, `dimensions.environment`,
and `dimensions.service`. Each bucket carries:

- `source_state_counts`: a count per provider-neutral source-state taxonomy
  value (`exact`, `derived`, `partial`, `ambiguous`, `stale`, `unavailable`,
  `unsupported`, `unknown`, `rejected`). The full vocabulary is always present;
  `unsupported`, `stale`, and `unavailable` are never folded into a clean total.
  See the [source-state taxonomy](../replatforming-source-state-taxonomy.md).
- `readiness`: `import_ready`, `needs_review`, and `refused` counts. These
  mirror the row-level Terraform import-plan outcomes, so the rollup's
  `import_ready` total agrees with that surface for the same findings. A
  safety-gate refusal is counted as `refused`, never `import_ready`.

Attribution is never guessed. A finding with more than one distinct service or
environment candidate is counted under the explicit `__ambiguous__` bucket key;
a finding with no candidate is counted under `__unattributed__`. The
account-wide `source_state_totals` and `readiness_totals`, plus
`recommended_next_checks`, give an operator a single at-a-glance adoption and
readiness view. When `truncated` is true the rollup covers only the bounded
page; re-run with `offset` or a tighter scope for a full rollup.

### Replatforming rollups observability

No-Observability-Change: this read reuses the shared query-handler instrumentation.
The `query.replatforming_rollups` span carries only the stable `http.route` and
`eshu.capability` attributes; per-resource identities (ARNs, account-scoped IDs)
stay out of span and metric labels. Operators read drift and readiness at a
glance from the bounded response fields (`source_state_totals`,
`readiness_totals`, and per-dimension buckets) rather than from a new
high-cardinality metric, keeping resource identities in traces and logs, not
metric labels.

## Content

Portable content lookup uses `repo_id + relative_path`; portable entity lookup
uses `entity_id`. File and entity read responses include `source_backend`.

Deployed HTTP runtimes are PostgreSQL-first for direct content reads. They
return `source_backend=unavailable` instead of reading from a server workspace
checkout when PostgreSQL is disabled or missing a row. Content search requires
the PostgreSQL content store.

Content search `limit` defaults to 50, is capped at 200, and uses `offset` with
a cap of 10000.

## Infrastructure And Impact

`/infra/resources/search` accepts optional `query` plus `category`, `kind`,
`provider`, `environment`, `resource_service`, `resource_category`, and
`limit`. Requests must include either non-empty `query` or at least one
structured filter. `category=cloud` searches canonical `CloudResource` nodes
from cloud collector evidence;
cloud results may include `arn`, `resource_id`, `account_id`, `region`, and
`service_kind`. Provider filters treat `source_system` as a provider fallback
only for `CloudResource` rows; source-system provenance on Terraform-state or
other non-cloud nodes is not returned as a cloud provider. Raw tag and
evidence payload values are not returned by this generic search route.
`limit` defaults to 50 and is capped at 200.

## Cloud Resource Inventory

`GET /api/v0/cloud/resources` is the bounded browse list for cloud-provider
resources projected as `CloudResource` graph nodes. It backs the console Cloud
page and any client that needs to page the full cloud inventory rather than
search one entity at a time.

Optional equality filters: `provider` (matched against `collector_kind`),
`resource_type`, `region`, and `account_id`. Unknown filter values simply
return no rows. `limit` defaults to 50 and is capped at 200.

Paging is keyset, not offset. The response orders by `resource_type` then `id`
and returns `truncated` plus `next_cursor` when more rows exist. To fetch the
next page, pass `next_cursor.after_resource_type` and `next_cursor.after_id`
back as the `after_resource_type` and `after_id` query parameters. The handler
fetches `limit + 1` rows to detect truncation without a count query, so deep
pages never use a `SKIP` scan.

Each row projects only fields present on the node: `id`, `resource_type`,
`name`, `provider`, `region`, `account_id`, `arn`, `service_name`, and `state`.
Empty optional fields are omitted from the wire payload, and a known
`service_name` placeholder is scrubbed so it never reaches a client. The
response carries the authoritative-graph truth envelope; when the active query
profile cannot serve the authoritative graph the route returns `501`, and `503`
when the graph backend is unavailable.

Performance Evidence: warm first-page latency for the handler Cypher shape
(`MATCH (n:CloudResource) RETURN <narrow projection> ORDER BY n.resource_type,
n.id LIMIT 51`) is ~0.9-1.0 ms against NornicDB over a corpus of 17,022
`CloudResource` nodes (NornicDB Bolt/HTTP at `127.0.0.1:7474`, database
`nornic`, default page `limit=50` so the executed `LIMIT` is 51). Warm keyset
resume (cursor predicate `n.resource_type > $after_resource_type OR
(n.resource_type = $after_resource_type AND n.id > $after_id)`) and warm
provider+resource_type filtered pages both measured ~1.0-1.1 ms; cold
first-touch on each shape was 0.43-0.84 s. The bounded `LIMIT 51` and keyset
predicate keep every page cheap regardless of depth into the 17k-node set, so
there is no whole-set scan or offset cost. Measurement commands are the warm-up
plus three timed `tx/commit` runs of the projection statement recorded on the
#1643 PR.

Observability Evidence: the route records the
`eshu_dp_cloud_resource_list_duration_seconds` histogram (outcome label `ok` or
`query_error`) and the `eshu_dp_cloud_resource_list_errors_total` counter
(reason label), both registered in `go/internal/telemetry/instruments.go`, plus
the `query.cloud_resource_list` request span declared in
`go/internal/telemetry/contract.go`. An operator can chart page latency and the
error rate, and trace a slow page through the span, without any new
high-cardinality metric dimension.

Legacy impact routes accept `limit` with default 50 and cap 200, probe one
extra row, and return `truncated`.

`/impact/change-surface/investigate` accepts one graph target family
(`target` + `target_type`, `service_name`, `workload_id`, `resource_id`, or
`module_id`) and/or a code scope (`topic`, `repo_id`, `changed_paths`).
`changed_paths` requires `repo_id`. `max_depth` defaults to 4 and caps at 8;
`limit` defaults to 25 and caps at 100; `offset` caps at 10000.

`/impact/entity-map` requires `from` and accepts `from_type`, `repo_id`,
`environment`, `relationship`, `depth`, and `limit`. `depth` defaults to 1 and
caps at 4; `limit` defaults to 25 and caps at 100.

The handler normalizes supported prefixes and resolves exactly one typed start
entity before traversal. Ambiguous selectors return candidates without graph
expansion. The default `depth=1` path uses direct incoming and outgoing
relationship-family queries, so high-cardinality repository edges do not expand
before `limit` can bound the result. Deeper requests remain bounded by the same
depth and limit caps, with coverage and truncation fields in the response.
For `depth>1`, the handler runs at most one direct and one deeper traversal per
direction instead of one graph query per relationship family.

No-Regression Evidence: `go test ./internal/query -run
'TestEntityMap' -count=1` covers the console call shape (`from` plus
`depth=2` and default limit) using resolver plus at most four bounded traversal
queries. The regression replaces the previous 22 sequential graph reads for a
service/workload anchor with four direction/depth reads while preserving direct
edge relationship verbs, typed anchors, limit+1 truncation, and repository
structural-fanout exclusions.

Observability Evidence: entity-map still emits the existing `query.entity_map`
handler span and graph query spans, and now annotates the handler span with
`eshu.entity_map.depth`, `eshu.entity_map.limit`,
`eshu.entity_map.relationship_filter`, `eshu.entity_map.traversal_seconds`,
`eshu.entity_map.result_count`, `eshu.entity_map.truncated`, and
`eshu.entity_map.traversal_error`. Operators can distinguish resolver success,
bounded traversal duration, result size, truncation, and graph-read errors
without new high-cardinality metric labels.

Supported first-slice handles include workload and service IDs/names, workload
instances, repositories, cloud resources, Terraform resources/data
sources/modules, Kubernetes resources, and graph file paths.

`/impact/resource-investigation` accepts `query` or `resource_id`, optional
`resource_type`, `environment`, `max_depth`, and `limit`. `resource_id` may be
the canonical graph resource id, provider resource id, or cloud ARN returned by
`/infra/resources/search`. `max_depth` defaults to 4 and caps at 8; `limit`
defaults to 25 and caps at 100.

When the target resource resolves but no workload usage edge or repository
provenance path is materialized, the response keeps `workloads` and
`provisioning_paths` empty and reports structured `missing_evidence` instead of
inventing an attachment.

`/compare/environments` requires `workload_id`, `left`, and `right`; optional
`limit` defaults to 50 and caps at 200. Config and runtime-setting drift are
reported as limitations when not materialized by the route.
