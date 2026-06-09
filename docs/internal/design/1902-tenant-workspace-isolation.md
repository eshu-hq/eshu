# Tenant And Workspace Isolation For Hosted Eshu (#1902)

Status: **PROPOSED - SECURITY AND SCHEMA REVIEW REQUIRED BEFORE RUNTIME ENFORCEMENT.**

Refs #1902. Refs #1774, #1852, #1899, #1900, #1907, #1910. See also
[Hosted Governance Policy Model](1900-hosted-governance-policy-model.md),
[Hosted Governance Posture](../../public/operate/hosted-governance.md),
[Hosted Project Onboarding](../../public/deployment/hosted-onboarding.md),
[System Architecture](../../public/architecture.md), and
[Postgres Ownership Inventory](1286-postgres-ownership-inventory.md).

This is a maintainer design gate. It defines the model and implementation
contract for tenant and workspace isolation. It does not change runtime
behavior, storage schema, graph writes, query handlers, MCP tools, Helm values,
or onboarding output by itself.

## Decision

Hosted isolation is an authorization boundary over the existing facts-first
pipeline. The durable intake key remains `ingestion_scopes.scope_id`; tenant
and workspace state grants subjects access to explicit scope and repository
sets. Query, MCP, semantic, collector, extension, and workflow decisions must
carry the effective tenant, workspace, policy revision, subject class, and
allowed scope set before doing work in `hosted_multi_tenant` mode.

Isolation must be enforced before read or claim execution, not by filtering a
completed response. Reducers still own graph truth and must not rewrite graph
state differently for each caller. Query surfaces must use tenant/workspace
authorization to choose the bounded graph or Postgres read shape.

The v1 model has three modes:

| Mode | Behavior |
| --- | --- |
| `local_no_policy` | No tenant state is required. Local deterministic reads may remain unscoped, and optional governed work stays disabled unless a narrower local policy allows it. |
| `hosted_single_tenant` | One explicit tenant/workspace may be configured. Existing shared-token behavior is still not per-team isolation; status must keep saying so until #1852 lands scoped tokens. |
| `hosted_multi_tenant` | Every non-public request, hosted claim, semantic job, extension activation, audit event, and status decision must name a tenant/workspace boundary and a policy revision before work proceeds. Missing or stale tenant scope denies governed work. |

## Identity Model

Use opaque, stable identifiers internally. Public status and issue/PR evidence
must not expose tenant names, workspace names, raw repository names, source
identifiers, credential handles, private endpoints, prompt text, provider
responses, or token values.

| Entity | Purpose | Required stable key |
| --- | --- | --- |
| Tenant | Commercial or operational isolation root. | `tenant_id`, opaque and immutable. |
| Workspace | Team/project boundary inside one tenant. | `(tenant_id, workspace_id)`. |
| Subject | Caller, service principal, collector, extension instance, or admin actor. | `subject_id_hash` plus `subject_class`. |
| Scope grant | Authorization to read or plan work for source scopes. | `(tenant_id, workspace_id, scope_id)`. |
| Repository grant | Optional finer-grained grant for Git repository reads. | `(tenant_id, workspace_id, repo_id)`. |
| Policy revision | Versioned governance source that allowed or denied the decision. | `policy_revision_hash`. |

`scope_id` is the primary authorization key because it already anchors
`ingestion_scopes`, `scope_generations`, `fact_records`, hosted collector
targets, freshness triggers, and many reducer readiness checks. `repo_id` is
allowed as a narrower grant for Git content and repository routes, but repo
grants must resolve back to active scope/generation truth before graph or
content reads run.

## Persistence Surfaces

The implementation must add tenant/workspace state as additive Postgres schema
before enforcement:

| Surface | Required isolation impact |
| --- | --- |
| `tenants`, `workspaces` | Durable state, status, tombstone/offboarding fields, and redacted display handles. |
| Subject/token registry | Scoped-token rows from #1852 map presented tokens to subject class, tenant, workspace, status, expiry, and policy revision. Only token hashes are stored. |
| `tenant_scope_grants` | Active grants to `ingestion_scopes.scope_id`, with grant source, revision hash, effective timestamps, and tombstone state. |
| `tenant_repository_grants` | Optional repo-level grants for Git routes. Must join to a scoped source before a read runs. |
| `ingestion_scopes` | Existing rows are not rewritten for v1. A future migration may add nullable `tenant_id` and `workspace_id` only after compatibility proof; until then grants reference existing `scope_id`. |
| `fact_records` | Existing `scope_id` and generation indexes remain the fact isolation anchor. New tenant indexes must be justified by measured query shapes, not added speculatively. |
| Content tables | Content reads must accept an authorized repo/scope set and apply it inside SQL predicates. Corpus-wide content search must be disabled or require all-scope authorization in multi-tenant mode. |
| Graph backend | Graph nodes/edges may carry scope/repo metadata already projected by reducers. Query Cypher must add selective scope/repo predicates. Do not fork graph truth per tenant. |
| Workflow tables | `workflow_runs`, `workflow_work_items`, `workflow_claims`, and `collector_instances` must carry or derive tenant/workspace/policy revision before claim planning and claim completion. |
| Semantic queue/jobs | Jobs must carry tenant/workspace, source scope, source class, provider profile, redaction, retention, egress decision, and policy revision. |
| Governance audit sink | Audit events must record tenant/workspace classes as hashes or bounded classes only, with decision, reason, policy revision, and correlation id. |
| Status rows | Public/API/MCP status exposes aggregate counts and reason classes only; detailed tenant state stays in private operator storage. |

## Read Enforcement

Read authorization has two phases:

1. Authentication resolves a subject and grants. Shared-token mode resolves to a
   synthetic all-scope subject only when the deployment is not enforcing
   multi-tenant isolation.
2. Handlers pass an `AuthorizationScope` into graph and content ports. The port
   applies the allowed scope/repo predicate before any limit, aggregate, cursor,
   or truncation is computed.

Required changes for implementation PRs:

- `AuthMiddleware` or its replacement attaches an auth context with subject
  class, tenant/workspace, all-scope flag, allowed scope ids, allowed repo ids,
  policy revision, and reason for denial.
- `GraphQuery` and graph-backed helper APIs accept authorization parameters or
  a context value that every handler must explicitly use. Hidden global filters
  are not acceptable because they are hard to review and benchmark.
- `ContentStore` gains scoped variants for repository list, repository resolve,
  file/entity reads, content search, coverage, language inventory, and
  aggregate reads.
- API and MCP errors use `permission_denied` for authenticated out-of-scope
  requests. They must not leak whether an out-of-scope repository exists
  through counts, truncation, timing-class text, or partial evidence.
- Shared-token compatibility stays byte-identical in `local_no_policy` and
  `hosted_single_tenant` unless a scoped-token mode is explicitly enabled.

## Claim And Runtime Enforcement

The workflow coordinator must make tenant scope a planning predicate, not a
collector runtime afterthought.

| Runtime | Required behavior |
| --- | --- |
| Workflow coordinator | Intersect enabled collector instances with tenant/workspace grants, egress policy, extension policy, and policy revision before creating runs or work items. Denied targets create no claimable row and emit a bounded decision reason. |
| Hosted collectors | Resolve claimed `scope_id` back to a configured target and tenant/workspace grant before fetching. Completion must reject stale policy revision or revoked scope before fact commit. |
| Scanner workers | Treat workspace and repo grants as the maximum target set. Resource-heavy analysis cannot broaden beyond the claim target. |
| Semantic extraction | Plan jobs only after read authorization, source ACL, semantic policy, egress, redaction, retention, and budget all allow the same tenant/workspace boundary. |
| Extension host | Re-check tenant/workspace, activation, isolation profile, and policy revision before launch and before fact commit. Revoked work must fail terminal with a bounded reason. |
| Reducer | Preserve source facts and canonical graph truth. It may consume tenant/workspace anchors from facts once implemented, but it must not infer authorization from repository names or caller identity. |

Concurrency contract:

- claim identity must include the effective tenant/workspace boundary where the
  same external scope can be granted to more than one workspace;
- revocation wins over old claims on the next policy check;
- retries must use stable idempotency keys that include scope, generation,
  collector kind, policy revision, and tenant/workspace when applicable;
- stale claims created under a prior policy revision cannot commit new facts
  after revocation.

## Migration And Compatibility

Implementation must be additive:

1. Add tenant/workspace and grant tables with no change to existing behavior.
2. Add shared-mode synthetic auth context and prove current API/MCP responses
   match existing behavior.
3. Add scoped-token registry from #1852 behind `ESHU_TENANCY_MODE=scoped`.
4. Enforce canary repository list and repository resolve routes first.
5. Expand to every graph, content, status, semantic, MCP, and collector claim
   path only after the canary proves parity and no leakage.

Existing single-tenant deployments must keep working without configuring every
tenant row. Operators may create one explicit tenant/workspace later to move
from `hosted_single_tenant` shared-token posture to scoped tokens.

Deleted tenants and workspaces must tombstone grants first, stop new claims,
revoke scoped tokens, keep audit/retention state according to hosted retention
policy, and preserve enough private operator evidence for recovery. Graph data
is rebuildable projection state; deletion semantics must follow the hosted
retention and graph-rebuild policy rather than deleting Postgres facts blindly.

## Required Proof Matrix

Implementation PRs must prove these cases before enforcement is accepted:

| Case | Required proof |
| --- | --- |
| Duplicate repository names across tenants | Team A and Team B can each read only its granted repo; corpus counts and language inventories do not reveal the other repo. |
| Out-of-scope repository selector | API and MCP return `permission_denied` or `not_found` without existence, count, or truncation leakage. |
| Stale scope generation | Scoped reads use active generation truth and do not revive retired facts. |
| Deleted tenant or workspace | Tokens and claims deny new work; retention/audit state remains diagnosable. |
| Partial migration | Shared mode remains unchanged; scoped mode refuses routes not yet enforcement-covered. |
| Retry and idempotency | Duplicate claim planning and collector retries converge on one intended work item/fact set. |
| Policy revision change | Old claims cannot commit after revocation; new claims use the latest valid revision. |
| API/MCP parity | Same token and selector produce the same allow/deny and truth metadata across HTTP, MCP, and CLI read paths. |
| Negative leakage | Canaries do not appear in logs, metrics, status, audit, API/MCP bodies, onboarding artifacts, or docs. |

## Implementation Split

This design intentionally leaves runtime changes to child issues and PRs:

| Slice | Existing or needed issue |
| --- | --- |
| Scoped token registry, auth context, and shared-mode compatibility | #1852 |
| Tenant/workspace schema and grant store | #2047 |
| Repository-list and repository-resolve canary enforcement | #2048 |
| Full query and MCP enforcement fan-out | #2049 |
| Workflow coordinator and collector claim tenant fencing | #2050, also related to #1907 |
| End-to-end two-team proof suite | #1910 |

If implementation uncovers a need for durable policy-revision activation,
operator admin routes, or tenant offboarding retention workers that are not
covered by those slices, create another bounded child issue before adding code.

## Evidence For This PR

No-Regression Evidence: docs-only design gate; no Go, Cypher, schema,
OpenAPI, MCP, Helm, Compose, queue, storage, graph, or runtime-default files are
changed.

No-Observability-Change: docs-only design gate; it defines future telemetry and
audit requirements but emits no metrics, spans, logs, status fields, or pprof
signals.

Source check date: 2026-06-09.

Sources used:

- `docs/internal/design/1900-hosted-governance-policy-model.md`
- `docs/public/deployment/hosted-onboarding.md`
- `docs/public/operate/hosted-governance.md`
- `docs/public/architecture.md`
- `docs/public/deployment/service-runtimes.md`
- `docs/public/reference/local-testing.md`
- `docs/public/reference/telemetry/index.md`
- `docs/public/reference/cypher-performance.md`
- `docs/public/reference/nornicdb-tuning.md`
- `docs/public/reference/nornicdb-pitfalls.md`
- `docs/internal/design/1286-postgres-ownership-inventory.md`
- `go/internal/query/ports.go`
- `go/internal/query/auth.go`
- `go/internal/query/README.md`
- `go/internal/storage/postgres/README.md`
- `go/internal/workflow/README.md`
- `schema/data-plane/postgres/001_ingestion_scopes.sql`
- `schema/data-plane/postgres/003_fact_records.sql`
