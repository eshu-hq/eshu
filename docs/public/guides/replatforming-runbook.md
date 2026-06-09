# Hosted Replatforming Runbook

Replatforming is a hosted operator workflow that turns Eshu's code-to-cloud
context graph into review-ready Terraform import candidates. Use this runbook to
move from a first AWS inventory to a planned migration without assuming Eshu ever
touches your cloud.

The workflow has five stages: **observe -> compare -> plan -> review -> apply
externally**. Eshu owns observe, compare, and plan. Review is a human gate.
Apply happens outside Eshu, under operator control.

For setup, read [Index Repositories](../use/index-repositories.md),
[Connect MCP](../mcp/index.md), and the [MCP Guide](mcp-guide.md). The exact
request and response contracts for every tool below live in the
[MCP Reference](../reference/mcp-reference.md) and the
[HTTP IaC, Content, And Infra Routes](../reference/http-api/iac-content-infra.md).

## What Eshu Does Not Do

These are hard, designed boundaries. They hold at every stage of this runbook.

- Eshu does **not** run Terraform, `terraform import`, `terraform apply`, or any
  provider mutation.
- Eshu does **not** import resources, mutate cloud state, or write Terraform
  state.
- Eshu does **not** write to your repositories.
- Eshu does **not** promote a provider observation to ownership truth without
  reducer-owned evidence.
- Eshu does **not** expose raw tags as ownership truth, and it redacts
  credential-like values. Secrets, state locators, private URLs, raw provider
  payloads, and credential names are not returned.

Every management and import-plan response is read-only and carries a
`safety_gate`. The import-plan surface returns refused candidates with refusal
reasons rather than silently omitting unsafe actions.

## Prerequisites

The AWS management and drift tools read **reducer-materialized AWS runtime drift
findings**. They require the `local_authoritative` profile or higher; lower
profiles receive `501 unsupported_capability`. Their answers are capped at the
`derived` truth level, never `exact`.

Before running this workflow, confirm:

1. **AWS cloud collection** is configured and has run. See
   [AWS Cloud Collector](../services/collector-aws-cloud.md) and the
   [AWS Cloud Coverage Matrix](../services/collector-aws-cloud-coverage-matrix.md).
2. **Terraform-state collection** is configured for the accounts you plan to
   migrate. See [Terraform State Collector](../services/collector-terraform-state.md).
   Without Terraform state, every cloud resource looks unmanaged.
3. The **reducer** has materialized AWS runtime drift findings. The reducer joins
   observed AWS resources, Terraform state, and owner-resolved Terraform config by
   ARN, then emits durable facts. Confirm readiness with the proof mindset in
   [Collector And Reducer Readiness](../reference/collector-reducer-readiness.md).

If those facts are missing or building, the tools return empty or partial pages.
Treat `building`, `stale`, or `unavailable` freshness as not-yet-answerable, not
as "nothing to migrate".

## Scope Every Call

These tools are account-scoped or scope-scoped on purpose. Every list and
import-plan call requires `scope_id` **or** `account_id`:

- `scope_id` is an exact AWS collector scope, for example
  `aws:123456789012:us-east-1:lambda`.
- `account_id` is a 12-digit AWS account ID. `region` narrows it further when
  supplied.
- `finding_kinds` filters to `orphaned_cloud_resource`,
  `unmanaged_cloud_resource`, `unknown_cloud_resource`, or
  `ambiguous_cloud_resource`.
- `limit` defaults to 100 and is capped at 500. `offset` pages through results.
  Responses carry `truncated` and `next_offset`; page until `truncated` is
  false.

The single-resource status tools also require `arn` or `resource_id`. For AWS,
`resource_id` is an alias for the full ARN, not a provider-local name such as an
S3 bucket name or Lambda function name.

## Stage 1 - Observe: Inventory

Start broad, then narrow. Read counts and groups before pulling per-resource
detail.

| Goal | MCP tool | HTTP route |
| --- | --- | --- |
| Unmanaged cloud resources | `find_unmanaged_resources` | `POST /api/v0/iac/unmanaged-resources` |
| Active AWS runtime drift | `list_aws_runtime_drift_findings` | `POST /api/v0/aws/runtime-drift/findings` |

For the authoritative Terraform/IaC inventory itself, the HTTP route
`GET /api/v0/iac/resources` is a bounded, keyset-paginated browse over the IaC
graph projection. See
[IaC Inventory](../reference/http-api/iac-content-infra.md#iac-inventory).

`find_unmanaged_resources` finds AWS cloud resources whose active reducer drift
facts show no Terraform config owner or only Terraform state ownership. The
response groups findings (`finding_groups`), carries a `safety_summary`, and
returns `findings` with `management_status`, `confidence`, matched Terraform
addresses, `service_candidates`, `environment_candidates`, `missing_evidence`,
`warning_flags`, and a per-finding `safety_gate`.

`list_aws_runtime_drift_findings` returns the same findings with an `outcome`
(`exact`, `derived`, `ambiguous`, `stale`, or `unknown`) and a
`promotion_outcome` (`not_promoted` or `rejected`). A `rejected` promotion means
the read-only finding must **not** drive Terraform import or cleanup automation.

## Stage 2 - Compare: Management Status

For any single resource, resolve its management status before planning anything.

| Goal | MCP tool | HTTP route |
| --- | --- | --- |
| One resource's status and gate | `get_iac_management_status` | `POST /api/v0/iac/management-status` |
| Grouped evidence explanation | `explain_iac_management_status` | `POST /api/v0/iac/management-status/explain` |

`get_iac_management_status` returns the `management_status`, the `finding`, and
the `safety_gate` for one ARN. `explain_iac_management_status` adds grouped
`evidence_groups` (cloud, Terraform, raw-tag, and management evidence) so a
reviewer can see why the status was assigned.

Management status values:

| Status | Meaning |
| --- | --- |
| `managed_by_terraform` | Cloud resource matched to Terraform config and state. |
| `terraform_state_only` | In Terraform state but no config owner resolved. |
| `terraform_config_only` | In Terraform config but not observed in state. |
| `cloud_only` | Observed in cloud with no Terraform ownership; an import candidate. |
| `managed_by_other_iac` | Managed by non-Terraform IaC. |
| `ambiguous_management` | Conflicting evidence; review required. |
| `unknown_management` | Insufficient evidence; review required. |
| `stale_iac_candidate` | IaC evidence is stale; review required. |

Raw tag values that look credential-like are redacted as `[REDACTED]` and the
`safety_gate.redactions` list records that a value was withheld. Raw tags are
provenance evidence only; they never infer environment or ownership truth.

## Stage 3 - Plan: Import Candidates

| Goal | MCP tool | HTTP route |
| --- | --- | --- |
| Terraform import candidates | `propose_terraform_import_plan` | `POST /api/v0/iac/terraform-import-plan/candidates` |

`propose_terraform_import_plan` generates read-only Terraform import-plan
candidates from the bounded findings **without running Terraform or mutating
cloud state**. Each candidate has a `status` of `ready` or `refused`:

- A `ready` candidate carries a `terraform_resource_type`, an `import_id`, a
  `suggested_resource_address`, and an `import_block` (HCL `import { ... }`). The
  response also returns a combined `terraform_import_plan` artifact of all ready
  blocks.
- A `refused` candidate carries `refusal_reasons`. Common reasons:
  `security_review_required`, `management_status_not_importable` (the status is
  not `cloud_only`), `unsupported_resource_type`, and `missing_provider_import_id`.

Import blocks are emitted only for safety-approved `cloud_only` findings in
**supported AWS families**. Today that is `aws_s3_bucket` and
`aws_lambda_function`. Ambiguous, unknown, stale, state-only, security-review,
and unsupported findings are returned as refused candidates so nothing unsafe is
hidden. The response includes `ready_count` and `refused_count` so you can size a
migration wave by what is actually plannable.

Each candidate also carries a `destination_hint` (a matched module path, config
file, or `operator_selected_target`) and a `configuration_shape`
(`module_shaped` or `flat_starter`) to help a reviewer place the resource.

### Ownership and blast radius

To order a migration wave by impact rather than count, pair the findings with
Eshu's impact surfaces. `service_candidates` and `environment_candidates` on
each finding are candidates, not promoted ownership. Use
`trace_resource_to_code` and `find_blast_radius` to understand what a resource
connects to before you sequence its import. These are read-only graph reads; see
the [MCP Reference](../reference/mcp-reference.md).

## Stage 4 - Review: The Human Gate

The `safety_gate` on every finding and candidate is the contract for this stage.

- `outcome` is `read_only_allowed` or `security_review_required`.
- `read_only` is always true. Nothing here mutates anything.
- `review_required` is true for `ambiguous_management`, `unknown_management`,
  and `stale_iac_candidate`, and for findings flagged
  `security_sensitive_resource`, `ambiguous_ownership`, `insufficient_coverage`,
  or `stale_iac_evidence`.
- When review is required, `refused_actions` lists `terraform_import_plan` and
  the import-plan surface refuses that candidate.
- `redactions` records any withheld sensitive values.
- `audit_expectation` states that callers should log the caller, scope, route,
  finding id, and safety outcome **without** resource secrets.

Before importing or applying anything outside Eshu, a human MUST inspect:

1. The `management_status` and `evidence_groups` for each candidate.
2. The `missing_evidence` and `warning_flags` lists; do not import on partial
   coverage.
3. The truth envelope: `truth.level` (these tools cap at `derived`),
   `truth.profile`, and `truth.freshness.state`. A `stale` or `building`
   freshness means the answer may not reflect current cloud or Terraform state.
4. The `suggested_resource_address` and `import_block` against the real target
   module and configuration. Eshu suggests an address; it does not pick your
   module layout.

## Stage 5 - Apply Externally

Eshu's output is review-ready, not applied. Terraform import and apply happen
**outside Eshu, under operator control**, in your own Terraform workflow:

1. Place the reviewed resource configuration in the target module.
2. Use the suggested `import_block` (or `terraform import`) in your Terraform
   tooling.
3. Run `terraform plan` and confirm a no-op or expected diff before `apply`.

Eshu never performs these steps and never receives the result of an apply except
through the next collection cycle, which re-observes cloud and Terraform state
and re-materializes drift findings.

## Starter Prompts

Use these with MCP, the API, or a graph-aware assistant. Fill in the account,
scope, region, and ARN you actually have.

### Account inventory

- "List unmanaged AWS resources for account `123456789012` in `us-east-1`, group
  them by finding kind, and show the safety summary."
- "Show active AWS runtime drift findings for account `123456789012`; page until
  there are no more, and tell me how many promotions were rejected."

### Service migration plan

- "Propose Terraform import candidates for account `123456789012` in
  `us-east-1`. Tell me the ready count, the refused count, and the refusal
  reason for each refused candidate."
- "For account `123456789012`, list `cloud_only` unmanaged resources, then for
  each ready import candidate show the suggested resource address, the import
  block, and the destination hint."

### Single-resource import review

- "Get the IaC management status for ARN
  `arn:aws:s3:::my-bucket` in account `123456789012`, then explain the grouped
  evidence and the safety gate."
- "For ARN `arn:aws:lambda:us-east-1:123456789012:function:my-fn`, show the
  management status, the missing evidence, the warning flags, and whether the
  safety gate allows an import-plan candidate or requires security review."

## Safety And Truth Recap

- Every tool here is read-only and account- or scope-bounded.
- Answers cap at `derived` truth and require the `local_authoritative` profile.
- Check `truth.freshness.state` before treating an answer as complete.
- Refused candidates and `review_required` gates are signals to inspect, not to
  bypass.
- Terraform import and apply are external operator actions; Eshu observes,
  compares, and plans only.
