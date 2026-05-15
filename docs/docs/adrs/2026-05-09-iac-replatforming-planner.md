# ADR: IaC Re-platforming Planner

**Date:** 2026-05-09
**Status:** Proposed
**Authors:** Allen Sanabria
**Deciders:** Platform Engineering
**Related:**

- `2026-04-19-multi-source-correlation-dsl-and-collector-readiness.md`
- `2026-04-20-terraform-state-collector.md`
- `2026-04-20-aws-cloud-scanner-collector.md`
- `2026-04-20-multi-source-reducer-and-consumer-contract.md`
- `2026-05-09-optional-component-boundary.md` if this ADR is split from
  the component package-manager workstream later.
- GitHub issue: https://github.com/eshu-hq/eshu/issues/60

---

## Context

Eshu is becoming a code-to-cloud evidence graph. Today the strongest product
story is answering how code, deployment configuration, Terraform, Helm,
Kubernetes, Argo CD, Crossplane, and runtime topology relate to each other.

The Terraform-state and AWS cloud scanner collectors add a more valuable
re-platforming surface:

- Git/Terraform config says what the organization intended to manage.
- Terraform state says what Terraform believes it owns.
- AWS cloud scanning says what actually exists in the provider.
- Eshu's graph says which services, repos, environments, and deployment paths
  those resources are connected to.

That combination lets Eshu help teams move from manually managed or partially
managed cloud resources into governed infrastructure as code. The first
product should not promise to auto-generate perfect Terraform modules. The
safer and more useful product is an evidence-backed re-platforming planner
that explains what is unmanaged, ambiguous, stale, or already managed, then
gives a human or LLM enough context to create a Terraform import or module PR.

The planner gives Eshu a concrete re-platforming role: make the current estate
explainable before a platform team starts moving resources into a different
management model.

## Dependency Update (2026-05-10)

The Terraform-state collector ADR now separates discovery from ingestion. That
matters for this planner because repo-local `.tfstate` files can become useful
evidence without letting raw state flow through normal Git content storage.
The planner should consume only approved, redacted Terraform-state facts and
candidate metadata. It should not read raw state or treat an unapproved
candidate as proof of ownership.

The planner also depends on the shared target-scope and credential model used
by Terraform-state and AWS cloud scanner collectors. Re-platforming findings
should always say which account, region, role, backend, and evidence generation
produced the classification. Without that provenance, an LLM can draft code,
but the platform cannot explain why the draft is safe.

## Definitions

For this ADR, re-platforming means moving existing workloads and infrastructure
to a better operating model with selective reshaping, not a full rewrite. In
cloud migration terms, this is close to "lift, tinker, and shift": preserve
the business capability while improving the platform, management model, or
runtime target.

For Eshu, the first re-platforming target is IaC adoption:

- move unmanaged AWS resources into Terraform ownership
- move one-off Terraform resources into standard modules
- identify resources that should stay unmanaged, be retired, be imported, or
  be rebuilt
- expose evidence through MCP so an assistant can generate a reviewable PR

## Problem Statement

Organizations often have a mixed cloud estate:

- resources declared in Terraform and present in state
- resources in state with missing or moved configuration
- resources created by CloudFormation, CDK, Pulumi, SAM, Serverless Framework,
  or console/CLI operations
- resources that are live but cannot be tied to any known management system
- stale IaC that declares resources that no longer exist

The current manual workflow is slow and risky:

1. export cloud inventory
2. inspect Terraform repos and state
3. infer ownership from tags and names
4. decide whether to import, rewrite, ignore, or delete
5. write Terraform by hand
6. run import and plan commands
7. review drift and avoid destructive changes

LLMs can help write Terraform, but without a source-of-truth graph they guess.
They do not know which repo owns a resource, which module pattern is standard,
which resources are attached to production workloads, or whether an apparently
unmanaged resource is actually managed by another IaC system.

Eshu should provide that missing evidence layer.

## Decision

Introduce an **IaC Re-platforming Planner** capability on top of existing and
planned collectors. The planner is not a new source collector. It is a
reducer/query/MCP capability that consumes graph evidence from Git, Terraform
state, AWS cloud scanner, and later additional cloud/IaC collectors.

The first release should be read-only and evidence-first:

1. classify cloud resources by IaC management status
2. explain the evidence behind each classification
3. group unmanaged and ambiguous resources by service, environment, account,
   region, tags, and dependency path
4. generate Terraform import-plan candidates
5. expose the result through API and MCP for Codex, Claude, or another
   assistant to draft a pull request

Eshu must not apply Terraform, mutate cloud resources, or silently create
infrastructure code in the first version.

## Management Status Model

Avoid the vague label "not managed by IaC" as the only output. The planner
should emit explicit states with confidence and evidence.

| Status | Meaning |
| --- | --- |
| `managed_by_terraform` | Resource is matched to Terraform config and Terraform state. |
| `terraform_state_only` | Resource is present in Terraform state but config evidence is missing or ambiguous. |
| `terraform_config_only` | Resource is declared in Terraform config but not observed in state or cloud. |
| `cloud_only` | Resource is observed in cloud inventory with no matching Terraform state or known IaC evidence. |
| `managed_by_other_iac` | Resource appears managed by CloudFormation, CDK, Pulumi, Crossplane, or another detected system. |
| `ambiguous_management` | Multiple possible ownership signals conflict or are too weak. |
| `unknown_management` | Eshu lacks enough coverage, permissions, or source evidence to classify safely. |
| `stale_iac_candidate` | IaC evidence exists, but live cloud evidence is absent or inconsistent. |

Every result should include:

- status
- confidence
- matched evidence rows
- missing evidence
- recommended next action
- warning flags

## Proposed Read Model

The reducer should produce a read model similar to:

```text
IaCManagementFinding
  finding_id
  provider
  account_id
  region
  resource_type
  resource_id
  arn
  tags
  management_status
  confidence
  matched_terraform_state_address
  matched_terraform_config_file
  matched_terraform_module_path
  matched_other_iac_source
  service_candidates
  environment_candidates
  dependency_paths
  evidence
  warnings
  recommended_action
```

The evidence list should be drill-down friendly for MCP and API consumers:

- Git file path and entity ID when config evidence exists
- Terraform state address, serial, lineage, and backend locator when state
  evidence exists
- AWS ARN, account, region, service, tags, and source generation when cloud
  evidence exists
- relationship paths to workload, deployable unit, service, repo, and
  environment when known
- coverage gaps such as missing AWS permissions, missing state backend, or
  unsupported provider resource type

## MCP/API Surface

Add read-only tools before adding any code-generation workflow:

```text
find_unmanaged_cloud_resources(filter)
get_iac_management_status(resource_id)
explain_iac_management_status(resource_id)
get_iac_replatforming_candidates(service | repo | account | tag filter)
propose_terraform_import_plan(resource_ids | filter)
```

The MCP response must be useful to an LLM but grounded enough for humans:

- concise story first
- grouped findings next
- exact evidence rows last
- explicit limitations
- no hidden inference

The LLM should call Eshu for the evidence, then draft Terraform in the target
repo using normal coding tools. That keeps Eshu focused on evidence and lets
Codex, Claude, or another assistant own the PR drafting experience.

## Terraform Import And Generation Boundary

Terraform has native import workflows, including import blocks and generated
configuration flows. Eshu should produce inputs to those workflows, not replace
Terraform.

For a candidate resource, Eshu can produce:

- Terraform resource type
- provider alias/account/region hint
- import ID
- suggested resource address
- suggested module destination
- evidence and risk notes
- whether generated config should be flat starter code or module-shaped code

The generated artifact should be a plan candidate, for example:

```hcl
import {
  to = aws_lb.example
  id = "arn:aws:elasticloadbalancing:..."
}
```

Eshu should not run `terraform apply`. A human or CI job must run `terraform
plan`, review drift, and approve imports.

## Grouping And Module Suggestions

The planner should help decide whether resources belong in a reusable module
or a one-off import. Suggested grouping signals:

- shared tags such as `app`, `service`, `env`, `owner`, `team`, `cost_center`
- AWS relationships such as ALB -> target group -> ECS service -> task
  definition -> ECR image
- Terraform module patterns already used in indexed repos
- repo/service ownership from Git and deployment evidence
- environment boundaries from account, region, namespace, or naming
- graph paths from workload to dependency

The first version should label these as suggestions. It should not claim an
exact module boundary unless repo conventions and existing module evidence
make the boundary clear.

## Product Workflow

Target flow for a platform engineer:

1. Enable Git, Terraform-state, and AWS collectors.
2. Ask Eshu: "show cloud resources attached to payment-service that are not
   managed by Terraform."
3. Eshu returns grouped findings with evidence and confidence.
4. Engineer asks: "propose a Terraform import plan for the safe candidates."
5. Eshu returns import blocks, suggested addresses, destination repo/module,
   and warnings.
6. Codex or Claude uses Eshu MCP plus the repo checkout to draft a PR.
7. CI runs Terraform validation and plan.
8. Human reviews plan output and imports resources deliberately.

This workflow makes Eshu the system of evidence, not an unsafe automation
engine.

## Safety Rules

- Eshu must stay read-only for cloud and Terraform in the initial planner.
- No automatic Terraform apply.
- No automatic cloud mutation.
- No import recommendation without provider-specific import ID confidence.
- No module rewrite recommendation without evidence of existing module
  conventions or an explicit operator target.
- Do not collapse weak tag matches into exact ownership.
- Preserve `unknown_management` and `ambiguous_management` as first-class
  outcomes.
- Surface missing collector coverage and permissions as limitations.
- Treat security-sensitive resources specially: IAM, KMS, Secrets Manager,
  SSM parameters, security groups, public networking, and databases should
  require stronger review flags.

## Observability

The planner should expose:

- findings by management status
- findings by provider/account/region/resource family
- classification confidence distribution
- resources skipped due to missing permissions or unsupported types
- import-plan candidates generated
- warnings by class
- MCP/API latency for planner tools
- reducer backlog for IaC management classification

Metric labels must stay low-cardinality. Resource IDs, ARNs, repo names, and
state addresses belong in logs or evidence rows, not metric labels.

## Alternatives Considered

### Let the LLM infer everything from AWS and repo access

Rejected. LLMs are useful for drafting code, but they are not a durable source
of truth. Without graph evidence, they will overfit names, tags, or nearby
files and miss hidden dependencies.

### Generate Terraform modules automatically inside Eshu

Deferred. This may be useful later, but the first product should produce
evidence and import-plan candidates. Module generation should happen in a PR
workflow where a human can review the code and Terraform plan.

### Use CloudFormation IaC Generator as the product

Rejected as the core Eshu path. AWS CloudFormation IaC Generator is useful for
CloudFormation-centered workflows, but Eshu's target is cross-source evidence
and Terraform/module adoption across code, state, cloud, and runtime context.
AWS-native generation also does not know Eshu's repo graph, module patterns,
or service ownership.

### Build a standalone migration service first

Rejected for the first phase. The MCP/API read model is the smaller and safer
unit. A dedicated service can come later once the read model is proven.

## Consequences

Positive:

- gives Eshu a clear re-platforming story
- turns tfstate and AWS collectors into an actionable product workflow
- creates a strong MCP use case for Codex and Claude
- keeps dangerous mutations out of Eshu core
- gives platform teams a safe first step before writing Terraform

Negative:

- requires precise matching and truth labels to avoid bad import advice
- adds reducer/query complexity beyond raw collector facts
- needs provider-specific import ID rules
- may expose sensitive metadata if evidence redaction is weak
- will need careful docs so users understand this is a planning workflow, not
  an apply workflow

## Implementation Phases

### Phase 0: ADR And Issue Baseline

- Capture the planner concept and safety boundaries.
- Link the work to the Terraform-state and AWS collector epics.

### Phase 1: Read Model Design

- Define management statuses, confidence rules, evidence shape, and coverage
  limitations.
- Add contract tests using fixture facts from Git, Terraform state, and AWS.

Phase 1 implementation note:

- `IaCManagementFinding` is the query-facing read-model contract for the first
  AWS-backed slice. It keeps all eight management statuses stable:
  `managed_by_terraform`, `terraform_state_only`, `terraform_config_only`,
  `cloud_only`, `managed_by_other_iac`, `ambiguous_management`,
  `unknown_management`, and `stale_iac_candidate`.
- Promotion is evidence-gated. Raw tags are emitted as provenance evidence only
  and never become service, environment, or ownership truth by themselves.
- The current reducer-backed AWS runtime drift facts map
  `orphaned_cloud_resource` to `cloud_only` and
  `unmanaged_cloud_resource` to `terraform_state_only`. The read model already
  accepts matched Terraform state/config fields, other-IaC source, service and
  environment candidates, dependency paths, missing evidence, warning flags,
  and recommended actions so #130 can add stronger matching without changing
  the API shape.
- No-Regression Evidence: focused Go tests cover the management-status taxonomy,
  evidence-derived read-model enrichment, OpenAPI contract, and active
  generation Postgres read adapter without widening the bounded
  `scope_id`/`account_id` query.
- No-Observability-Change: this phase changes response shaping and optional
  decoded payload fields only. Existing `query.iac_management` Postgres spans
  from `InstrumentedDB`, the `query.iac_unmanaged_resources` handler span, and
  the bounded paging fields `limit`, `offset`, `truncated`, and `next_offset`
  remain the operator signals for this read path.

### Phase 2: Query And MCP Tools

- Add read-only API/MCP tools for unmanaged and ambiguous cloud resources.
- Return story-first answers with evidence rows and limitations.

### Phase 3: Terraform Import Plan Candidates

- Add provider-specific import ID mapping for the first AWS resource families.
- Generate import blocks and destination suggestions as text artifacts.
- Require explicit warnings for destructive or ambiguous imports.

### Phase 4: LLM-Assisted PR Workflow

- Document prompts and MCP workflows for Codex/Claude.
- Let the assistant draft PRs from Eshu evidence.
- Keep Terraform validation and plan in CI/human review.

### Phase 5: Dedicated Migration Planner Service

- Consider a separate service only if the MCP/API workflow proves valuable and
  operators need saved plans, approvals, or workflow state.

## Acceptance Criteria

- Eshu can classify AWS resources by Terraform/IaC management status using
  Git, Terraform state, and AWS evidence.
- Every classification includes confidence, evidence, and limitations.
- MCP/API tools can answer which cloud resources are unmanaged or ambiguous
  for a service, repo, account, region, or tag filter.
- Import-plan candidates include Terraform resource type, import ID, suggested
  address, destination hint, and warnings.
- Eshu never applies Terraform or mutates cloud resources.
- LLM workflows use Eshu MCP as evidence input and produce reviewable PRs.
- Docs explain the re-platforming workflow and safety model.
