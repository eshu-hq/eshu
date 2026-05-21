# HTTP IaC, Content, And Infra Routes

Use these routes for infrastructure cleanup candidates, source/content reads,
shared-infrastructure tracing, change impact, and environment comparison.

## IaC Quality Routes

- `POST /api/v0/iac/dead`
- `POST /api/v0/iac/unmanaged-resources`
- `POST /api/v0/iac/terraform-import-plan/candidates`
- `POST /api/v0/iac/management-status`
- `POST /api/v0/iac/management-status/explain`
- `POST /api/v0/aws/runtime-drift/findings`

## Dead IaC

`POST /api/v0/iac/dead`

Requires explicit `repo_id` or bounded `repo_ids`. When reducer-materialized
reachability rows exist, the route returns those rows with
`analysis_status=materialized_reachability`. Otherwise it falls back to bounded
indexed-content analysis for Terraform modules, Helm charts, Kustomize bases
and overlays, Ansible roles and playbooks, and Docker Compose services.

Used artifacts are omitted from cleanup findings. Unreferenced artifacts return
as `candidate_dead_iac`; variable or template-selected artifacts return as
`ambiguous_dynamic_reference` when `include_ambiguous=true`.

```json
{
  "repo_ids": ["terraform-stack", "helm-charts", "compose-app"],
  "families": ["terraform", "helm", "kustomize", "compose"],
  "include_ambiguous": true,
  "limit": 100,
  "offset": 0
}
```

## AWS Management And Runtime Drift

`POST /api/v0/iac/unmanaged-resources`

Reads active AWS runtime drift reducer facts. It requires `scope_id` or
`account_id`; `region`, `finding_kinds`, `limit`, and `offset` narrow the page.
Returned findings include AWS ARN, account, region, management status,
matched Terraform state/config fields when present, service and environment
candidates, dependency paths, missing evidence, warnings, reducer evidence,
recommended next action, and `safety_gate`.

`POST /api/v0/aws/runtime-drift/findings`

Exposes the broader drift read surface over the same active reducer facts. It
returns `drift_findings`, `outcome_groups`, bounded paging fields, and a truth
envelope for `aws_runtime_drift.findings.list`.

Raw tag values that look like passwords, tokens, credentials, secret values,
environment values, parameter values, or authorization material are returned as
`[REDACTED]`.

## Terraform Import Plan Candidates

`POST /api/v0/iac/terraform-import-plan/candidates`

Turns only safety-approved `cloud_only` findings for supported resource
families into Terraform `import` blocks. It does not run Terraform, write
state, or mutate cloud resources.

Security-review, ambiguous, unknown, stale, state-only, and unsupported
findings remain in the response as refused candidates so operators can see why
the route did not generate an import block.

For this route, `resource_id` is only an alias for `arn`. Pass the full AWS
ARN, not a provider-local ID such as an S3 bucket name or Lambda function name.

## Management Status

- `POST /api/v0/iac/management-status`
- `POST /api/v0/iac/management-status/explain`

These routes inspect one exact AWS stable resource identity. They require
`scope_id` or `account_id` plus `arn` or `resource_id`; for AWS,
`resource_id` should be the ARN.

Management status values are deterministic:

| Status | Meaning |
| --- | --- |
| `managed_by_terraform` | Cloud, Terraform state, and Terraform config evidence agree. |
| `terraform_state_only` | Cloud and Terraform state evidence agree, but config evidence is missing. |
| `terraform_config_only` | Terraform config evidence exists, but state and cloud evidence are absent. |
| `cloud_only` | Cloud evidence exists with no Terraform state, config, or other-IaC owner. |
| `managed_by_other_iac` | Cloud evidence is tied to non-Terraform IaC. |
| `ambiguous_management` | Ownership signals conflict or multiple IaC systems claim the resource. |
| `unknown_management` | Coverage, permissions, or supported-resource evidence is insufficient. |
| `stale_iac_candidate` | IaC evidence exists, but fresh cloud evidence is absent or inconsistent. |

Ambiguous and unknown outcomes are first-class statuses, not errors or silent
fallbacks.

## Content Routes

- `POST /api/v0/content/files/read`
- `POST /api/v0/content/files/lines`
- `POST /api/v0/content/entities/read`
- `POST /api/v0/content/files/search`
- `POST /api/v0/content/entities/search`

Rules:

- portable file lookup uses `repo_id + relative_path`
- content routes accept repository selectors in `repo_id` and `repo_ids`
- portable entity lookup uses `entity_id`
- deployed API runtimes are PostgreSQL-first and PostgreSQL-only for direct
  content reads
- deployed HTTP reads return `source_backend=unavailable` instead of reading
  from a server workspace checkout when PostgreSQL is disabled or missing a row
- local CLI and non-deployed helper flows may still use workspace or graph-cache
  fallbacks
- file and entity read responses include `source_backend`
- content search routes require the PostgreSQL content store

Example file read:

```json
{
  "repo_id": "payments",
  "relative_path": "src/payments.py"
}
```

Content search is bounded at the PostgreSQL query boundary. `limit` defaults to
50 and is capped at 200. `offset` is capped at 10000.

## Infra Routes

- `POST /api/v0/infra/resources/search`
- `POST /api/v0/infra/relationships`
- `GET /api/v0/ecosystem/overview`
- `POST /api/v0/impact/trace-resource-to-code`
- `POST /api/v0/impact/explain-dependency-path`
- `POST /api/v0/impact/blast-radius`
- `POST /api/v0/impact/change-surface`
- `POST /api/v0/impact/change-surface/investigate`
- `POST /api/v0/impact/entity-map`
- `POST /api/v0/impact/resource-investigation`
- `POST /api/v0/compare/environments`

Use these routes for shared infrastructure, blast radius, dependency
explanation, and environment drift.

Legacy entity-scoped impact routes accept `limit` with default 50 and cap 200.
They probe one extra graph row and return `truncated`.

## Change Surface Investigation

`POST /api/v0/impact/change-surface/investigate`

Accepts one graph target family (`target` + `target_type`, `service_name`,
`workload_id`, `resource_id`, or `module_id`) and/or a code scope (`topic`,
`repo_id`, `changed_paths`).

The handler resolves the target with exact, label-scoped graph lookups. Bare
`service_name` values also probe canonical `workload:<name>` before falling
back to bounded name or repo-scoped workload lookup. Ambiguous targets return
`target_resolution.status=ambiguous` and do not run traversal.

Resolved targets use bounded traversal with `max_depth` default 4 and cap 8;
`limit` defaults to 25 and caps at 100.

## Entity Map

`POST /api/v0/impact/entity-map`

This is the API contract behind `eshu map --from <thing>`. It requires `from`
and accepts optional `from_type`, `repo_id`, `environment`, `relationship`,
`depth`, and `limit`.

The handler normalizes supported prefixes and resolves exactly one typed start
entity before traversal. Ambiguous selectors return candidates without graph
expansion. The default `depth=1` path uses direct incoming and outgoing
relationship-family queries, so high-cardinality repository edges do not expand
before `limit` can bound the result. Deeper requests remain bounded by `depth`
cap 4, `limit` default 25 and cap 100, and response coverage/truncation fields.

Supported first-slice handles include workload and service IDs/names, workload
instances, repositories, cloud resources, Terraform resources/data
sources/modules, Kubernetes resources, and graph file paths.

## Resource Investigation

`POST /api/v0/impact/resource-investigation`

Use this for questions such as "what provisions this database" and "which
workloads depend on this queue." It accepts `query` or `resource_id`, optional
`resource_type` and `environment`, `max_depth` default 4 and cap 8, and `limit`
default 25 and cap 100.

## Environment Comparison

`POST /api/v0/compare/environments`

Accepts `workload_id`, `left`, `right`, and optional `limit` with default 50
and cap 200. The response includes a prompt-ready `story`, `summary`, shared
and dedicated resources, evidence, limitations, and recommended next calls.

The current contract is explicit that config and runtime-setting drift are not
materialized by this route yet; those gaps appear under `limitations`.

## Infrastructure Search

`POST /api/v0/infra/resources/search`

Accepts `query`, `category`, `kind`, `provider`, `resource_service`,
`resource_category`, and `limit`. `limit` defaults to 50 and is capped at 200.

Terraform AWS resource and data-source nodes preserve provider classification
in graph and content-backed responses, so callers can narrow searches to
families such as `provider=aws`, `resource_service=s3`, or
`resource_category=storage`.
