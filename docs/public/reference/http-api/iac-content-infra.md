# HTTP IaC, Content, And Infra Routes

Use these routes for IaC cleanup candidates, AWS runtime drift, source/content
reads, shared-infrastructure tracing, impact analysis, and environment
comparison. The route list is verified against `go/internal/query`.

## Route Map

| Area | Routes |
| --- | --- |
| IaC quality | `POST /api/v0/iac/dead` |
| AWS management and drift | `POST /api/v0/iac/unmanaged-resources`, `POST /api/v0/iac/management-status`, `POST /api/v0/iac/management-status/explain`, `POST /api/v0/iac/terraform-import-plan/candidates`, `POST /api/v0/aws/runtime-drift/findings` |
| Content | `POST /api/v0/content/files/read`, `POST /api/v0/content/files/lines`, `POST /api/v0/content/entities/read`, `POST /api/v0/content/files/search`, `POST /api/v0/content/entities/search` |
| Infrastructure | `POST /api/v0/infra/resources/search`, `POST /api/v0/infra/relationships`, `GET /api/v0/ecosystem/overview` |
| Impact | `POST /api/v0/impact/trace-resource-to-code`, `POST /api/v0/impact/explain-dependency-path`, `POST /api/v0/impact/blast-radius`, `POST /api/v0/impact/change-surface`, `POST /api/v0/impact/change-surface/investigate`, `POST /api/v0/impact/entity-map`, `POST /api/v0/impact/resource-investigation`, `POST /api/v0/compare/environments` |

OpenAPI remains canonical for full request and response schemas.

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

`/infra/resources/search` accepts `query`, `category`, `kind`, `provider`,
`resource_service`, `resource_category`, and `limit`. `category=cloud`
searches canonical `CloudResource` nodes from cloud collector evidence;
cloud results may include `arn`, `resource_id`, `account_id`, `region`, and
`service_kind`. Provider filters treat `source_system` as a provider fallback
only for `CloudResource` rows; source-system provenance on Terraform-state or
other non-cloud nodes is not returned as a cloud provider. Raw tag and
evidence payload values are not returned by this generic search route.
`limit` defaults to 50 and is capped at 200.

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
