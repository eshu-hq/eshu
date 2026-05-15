# HTTP API Reference

The HTTP API is versioned under `/api/v0` and shares the same query model as CLI and MCP. It is intended for AI agents, automation, and internal tools that need a stable contract.

## OpenAPI is the source of truth

The live OpenAPI spec is always canonical. If this page and the spec disagree, the spec wins.

- `GET /api/v0/openapi.json` - machine-readable schema
- `GET /api/v0/docs` - Swagger UI for trying requests in a browser
- `GET /api/v0/redoc` - ReDoc reference for reading the API contract

These documentation routes are intentionally public, like the OpenAPI JSON
schema. API data routes still follow the configured API auth policy.

For the mounted Go runtime admin surface, the checked-in OpenAPI contract lives
in `docs/openapi/runtime-admin-v1.yaml`. That contract is separate from the
public `/api/v0` schema because it belongs to the long-running runtime admin
endpoints, not the public query API.

## Response Envelope Contract

Responses use a canonical envelope when the client opts in by sending:

```http
Accept: application/eshu.envelope+json
```

Without that header, handlers keep emitting the legacy payload shape for
backwards compatibility. The envelope is the canonical contract for programmatic
clients, MCP, and CLI `--json` mode.

### Envelope shape

```json
{
  "data": { ... },
  "truth": {
    "level": "derived",
    "capability": "code_search.exact_symbol",
    "profile": "local_lightweight",
    "basis": "content_index",
    "freshness": { "state": "fresh" },
    "reason": "resolved from indexed entity and content tables"
  },
  "error": null
}
```

- `data` carries the response payload. `null` on error responses.
- `truth` carries the truth label for the response. `null` on error responses.
- `error` is `null` on success. On failure it carries a structured error
  envelope (see below).

### Truth levels

| Level | Meaning |
| --- | --- |
| `exact` | Authoritative graph or durable semantic truth. |
| `derived` | Deterministic result computed from indexed entities, content, or other structured relational state. |
| `fallback` | Exploratory result — useful but not strong enough to claim full authority for the requested capability. |

High-authority capabilities (transitive callers/callees, call-chain paths,
dead-code, cross-repo impact) do not silently downgrade to `fallback` in a
profile that cannot answer them correctly. They return a structured
`unsupported_capability` error instead.

### Freshness states

| State | Meaning |
| --- | --- |
| `fresh` | Answer reflects current indexed truth for the requested scope. |
| `stale` | Previously indexed truth exists but backlog or lag means the answer may be behind source. |
| `building` | Initial or replacement indexing is still in progress; authoritative data is not ready. |
| `unavailable` | Required backend or authoritative source is currently unavailable. |

Clients that cache responses MUST invalidate on changes to `truth.level` or
`truth.freshness.state`. ETags or cache keys should vary on both fields.

### Runtime profiles

`truth.profile` is one of:

- `local_lightweight` — single-binary `eshu` host with embedded Postgres, no
  authoritative graph backend.
- `local_authoritative` — local `eshu` service with embedded Postgres and
  NornicDB, authoritative graph available for the indexed workspace.
- `local_full_stack` — full Docker Compose stack, authoritative graph available.
- `production` — deployed multi-runtime platform.

Set the runtime profile via the `ESHU_QUERY_PROFILE` environment variable at
process start. Invalid values are rejected at startup; there is no silent
default.

### Capability IDs

`truth.capability` references the capability matrix at
`specs/capability-matrix.v1.yaml`. Full semantics live in
`reference/capability-conformance-spec.md` and
`reference/truth-label-protocol.md`.

### Structured error codes

On failure, `error` carries a structured envelope:

```json
{
  "error": {
    "code": "unsupported_capability",
    "message": "transitive callers require authoritative graph mode",
    "capability": "call_graph.transitive_callers",
    "profiles": {
      "current": "local_lightweight",
      "required": "local_authoritative"
    }
  }
}
```

Initial error code set:

| Code | When |
| --- | --- |
| `unsupported_capability` | Capability not supported in the current runtime profile. Returned as HTTP 501. |
| `unauthenticated` | Authentication is missing or invalid. Returned as HTTP 401. |
| `invalid_argument` | Request parameters are invalid or malformed. |
| `not_found` | Requested finding, packet, entity, repo, or workspace scope does not exist. |
| `permission_denied` | Caller cannot view the requested source, document, or evidence. |
| `backend_unavailable` | Authoritative backend (Neo4j / Postgres) is unreachable. |
| `index_building` | Initial indexing is in progress; authoritative data not ready. |
| `scope_not_found` | Requested entity, repo, or workspace scope does not exist. |
| `capability_degraded` | Capability supported but running under reduced fidelity (e.g. reducer lag). |
| `overloaded` | Runtime is saturated; request rejected rather than queued unboundedly. |
| `internal_error` | Eshu failed unexpectedly while serving a request. |
| `documentation_read_model_unavailable` | Documentation packet routes are mounted without the Postgres documentation read model. |

Details, freshness semantics, and MCP embedding live in
`reference/truth-label-protocol.md`.

## Scope

The public HTTP API exposes three operator-relevant surfaces:

- a read/query surface for context, code, infra, and content retrieval
- a small ingester control surface for runtime status and manual scan requests
- a checkpoint surface for index completeness and admin reindex requests

Use it to resolve entities, fetch context, search code, trace infra, compare environments, and inspect the deployed ingester state.

Use the CLI for local indexing workflows. Use the Helm runtime for
deployment-managed repository ingestion and steady-state sync.

## Health, Status, And Completeness

Health checks answer whether a process can serve. Completeness checks answer
whether the latest published Go checkpoint is finished.

- `GET /health` reports API process health after dependency
  initialization. It does not prove the latest index run finished.
- The hosted API runtime also mounts the shared service-local admin surface on
  the same listener:
    - `GET /healthz`
    - `GET /readyz`
    - `GET /admin/status`
    - `GET /metrics`
  Those routes are documented by `docs/openapi/runtime-admin-v1.yaml`, not the
  public `/api/v0` OpenAPI schema.
  The `/admin/status` JSON shape includes `registry_collectors` for OCI and
  package-registry runtimes. That section reports aggregate instance, active
  scope, 24-hour recent completed generation, last-completed,
  retryable/terminal failure, and bounded failure-class counts without registry
  hosts, repository paths, package names, tags, digests, account IDs, metadata
  URLs, or credentials. The same shape includes `aws_cloud_scans` for AWS cloud
  collector runtimes. That section reports per `(collector_instance_id,
  account_id, region, service_kind)` scanner status, commit status, API call
  count, throttle count, warning count, and budget/credential flags. When the
  result reaches the configured row cap, the payload sets
  `aws_cloud_scans_truncated` and reports that cap in `aws_cloud_scan_limit`.
  AWS Config/EventBridge freshness backlog appears under `aws_freshness` with
  aggregate `status_counts`, `oldest_queued_age`, and
  `oldest_queued_age_seconds`; event IDs, resource IDs, ARNs, and raw payloads
  stay out of the admin contract.
- `GET /api/v0/status/index` returns the current checkpoint summary.
- `GET /api/v0/index-status` returns the same checkpoint summary.
- `GET /api/v0/status/ingesters` is the canonical ingester-status list route.
- `GET /api/v0/status/ingesters/{ingester}` is the canonical ingester-status
  detail route.
- `GET /api/v0/ingesters` and `GET /api/v0/ingesters/{ingester}` return the
  same ingester-status payloads.
- `GET /api/v0/repositories/{repo_id}/coverage` returns durable repository
  coverage rows for one repository.
- Run-scoped completeness routes such as `/api/v0/index-runs/{run_id}` are not
  part of the shipped public contract. Do not assume the
  repository coverage route is run-scoped.
- `POST /api/v0/admin/refinalize` re-enqueues active scope generations for
  re-projection through the durable Go work queue.
- `POST /api/v0/admin/reindex` persists an asynchronous reindex request; the
  API process does not run the full reindex inline.
- `GET /api/v0/admin/shared-projection/tuning-report` returns the operator
  tuning report for shared-projection backlog behavior.
- `POST /api/v0/admin/replay`, `POST /api/v0/admin/dead-letter`,
  `POST /api/v0/admin/skip`, `POST /api/v0/admin/backfill`,
  `POST /api/v0/admin/work-items/query`, `POST /api/v0/admin/decisions/query`,
  and `POST /api/v0/admin/replay-events/query` expose the durable admin queue
  and decision controls.
- The service-local runtime admin surface remains separate from the public
  `/api/v0` contract even when it is mounted on the same listener. Use
  `/admin/status` when you need the runtime-local probe/status surface
  described by `docs/openapi/runtime-admin-v1.yaml`. Use `/admin/replay` and
  `/admin/refinalize` only on runtimes that mount the recovery handler, such
  as the ingester.

## Model Basics

- `workload` is the canonical deployable compute model.
- `service` is a convenience alias over workloads with `kind=service`.
- environment-scoped calls return the logical workload plus the resolved `WorkloadInstance`.
- canonical entity IDs are required on path-based context routes.
- repository identity is remote-first when a git remote exists.
- repository objects expose `repo_slug`, `remote_url`, and `local_path`.
- `local_path` is server-local metadata, not a portable client filesystem path.
- file-bearing query results should be interpreted using `repo_id + relative_path`, not an absolute server path.
- `repo_access` indicates whether the caller may need to ask the user for a local checkout path or clone decision.
- documentation-oriented clients should resolve canonical graph identity first, then use `repo_id + relative_path` or `entity_id` for exact evidence reads.
- repository-oriented context, summary, story, stats, and file routes accept a repository selector at the public boundary and normalize it to the canonical `repo_id` server-side.

### Deployment Evidence Pointers

Repository, workload, service, and deployment-trace responses may include
`deployment_evidence`. This object is intentionally compact: it returns counts
and grouped pointers instead of embedding full Postgres evidence payloads.

- `artifacts[]` carries the inspectable evidence pointer for one deployment,
  CI, IaC, or config signal.
- `artifacts[].resolved_id` is the durable lookup key for the
  `resolved_relationships` row in Postgres.
- `artifacts[].generation_id` identifies the relationship generation that
  produced the row.
- `artifacts[].source_location` identifies where the signal came from with
  `repo_id`, `repo_name`, `path`, and `start_line` / `end_line` when the
  extractor produced line data.
- `evidence_index.lookup_basis` is `resolved_id`.
- `evidence_index.relationship_types`, `evidence_index.artifact_families`, and
  `evidence_index.evidence_kinds` group artifact counts with the unique
  `resolved_ids` and `generation_ids` needed for drilldown.

`GET /api/v0/evidence/relationships/{resolved_id}` dereferences one pointer
into the durable relationship evidence row. The response includes
`lookup_basis: "resolved_id"`, source and target repo metadata, relationship
type, confidence, evidence count, evidence kinds, rationale, resolution source,
generation metadata, `evidence_preview`, and the decoded `details` JSON stored
with the resolved relationship. Use this endpoint when an API or MCP client
needs to explain why an edge exists without embedding the full evidence payload
in every graph-facing response.

### Documentation Truth Evidence

Documentation updater services should use the documentation truth routes
instead of reading graph internals directly.

`GET /api/v0/documentation/findings` lists read-only findings such as
`service_deployment_drift`. The endpoint accepts filters for `finding_type`,
`source_id`, `document_id`, `status`, `truth_level`, `freshness_state`,
`updated_since`, `limit`, and `cursor`. Each item includes the stable finding
identity, document and section identity, status, truth labels, summary, and an
`evidence_packet_url`.

`GET /api/v0/documentation/findings/{finding_id}/evidence-packet` returns the
bounded packet an external updater can snapshot before it plans a diff. The
packet includes finding identity, document and section metadata, bounded
excerpt, linked entities, current truth, evidence references, truth state,
permission state, and explicit `states` for stale, ambiguous, unsupported, or
ready findings. Eshu still does not draft text or write documentation; this
route only gives the updater the evidence it is allowed to use.

`GET /api/v0/documentation/evidence-packets/{packet_id}/freshness` lets an
updater check a saved packet before publishing a diff. If the packet is stale,
the updater should fetch the latest packet and restart planning from the new
snapshot.

### Package Registry Identity

Package registry routes expose the graph identity materialized from
`package_registry.package` and `package_registry.package_version` facts. They do
not report repository ownership or publication truth yet; source repository
hints remain provenance-only until reducer correlation admits them.

`GET /api/v0/package-registry/packages` lists package identities. The caller
must provide `limit` and either `package_id` or `ecosystem`. `name` may narrow
an ecosystem-scoped lookup. Responses include package identity fields,
`version_count`, `truncated`, and the requested `limit`.

`GET /api/v0/package-registry/versions` lists versions for one package. The
caller must provide `package_id` and `limit`. Responses include version
identity, publication timestamp when present, yank/unlist/deprecation flags,
`truncated`, and the requested `limit`.

## Context API

### Resolve fuzzy input into canonical entities

`POST /api/v0/entities/resolve`

Use this before context lookups when the caller has a fuzzy name, alias, or partial resource description.

```json
{
  "name": "payments-api",
  "type": "workload",
  "repo_id": "payments",
  "limit": 5
}
```

Responses include `entities`, `count`, the normalized `limit`, and
`truncated` so MCP clients can page or disambiguate before calling context
tools.

### Get canonical entity context

`GET /api/v0/entities/{id}/context`

Examples:

- `GET /api/v0/entities/workload:payments-api/context`
- `GET /api/v0/entities/workload-instance:payments-api:prod/context`

Entity context responses may also include semantic narrative fields when the
entity carries normalized semantic metadata:

- `semantic_summary`
- `semantic_profile`
- `story`

### Get workload context

`GET /api/v0/workloads/{id}/context`

Logical view:

- `GET /api/v0/workloads/workload:payments-api/context`

Environment-scoped view:

- `GET /api/v0/workloads/workload:payments-api/context?environment=prod`

### Get service context

`GET /api/v0/services/{id}/context`

This is an alias route. It still accepts a canonical workload ID:

- `GET /api/v0/services/workload:payments-api/context`
- `GET /api/v0/services/workload:payments-api/context?environment=prod`

Service alias responses include `requested_as=service`.

When a repository has workload identity facts but no materialized `Workload`
node yet, service context can fall back to the repository read model. Those
responses use `materialization_status=identity_only`,
`query_basis=repository_read_model`, an empty `instances` array, and a
`limitations` entry of `workload_identity_not_materialized`. Deployment
evidence and delivery-family paths may still be present when parser or
relationship evidence proves them.

## Story API

Use the story routes when the caller wants a structured narrative first and
evidence second.

Repository story responses expose a structured narrative contract. Service
story responses expose the one-call service dossier contract so MCP harnesses
and Console do not have to compose context, trace, relationship, and content
tools for the normal service explainer path. Workload story responses remain
narrative-first; use the deployment trace route when you need the richer
deployment-mapping contract for generic workload questions.

Repository story responses are shaped around:

- `subject`
- `story`
- `story_sections`
- optional `semantic_overview`
- `deployment_overview`
- `gitops_overview`
- `documentation_overview`
- `support_overview`
- `coverage_summary`
- `limitations`
- `drilldowns`

Within `deployment_overview`, repository story responses may also include:

- `delivery_family_paths`
- `delivery_family_story`
- `delivery_paths`
- `delivery_workflows`
- `shared_config_paths`

When repository entities carry semantic signals, repository story responses
also:

- add a `semantics` entry into `story_sections`
- embed semantic coverage text into the top-level `story`
- expose aggregated semantic counts in `semantic_overview`

Workload story responses are still shaped around:

- `subject`
- `story`
- optional lightweight identifiers such as `subject`

Service story responses are shaped around:

- `service_identity`
- `story`
- `story_sections`
- `api_surface`
- `deployment_lanes`
- `upstream_dependencies`
- `downstream_consumers`
- `evidence_graph`
- `investigation`
- `deployment_overview`
- `documentation_overview`
- `support_overview`
- `result_limits`

Deployment-oriented trace responses are shaped around:

- `subject`
- `story`
- `story_sections`
- `deployment_overview`
- `gitops_overview`
- `controller_overview`
- `runtime_overview`
- `deployment_sources`
- `cloud_resources`
- `k8s_resources`
- `image_refs`
- `k8s_relationships`
- `controller_driven_paths`
- `delivery_paths`
- `deployment_fact_summary`
- `drilldowns`

When repository-backed delivery-family synthesis is available, trace responses
may also surface those grouped summaries through `deployment_overview`, such
as `delivery_family_paths`, `delivery_family_story`, `delivery_workflows`, and
`shared_config_paths`.

When controller evidence is recoverable from the deployment repositories,
`controller_overview` may also include concrete controller entity records in
`entities`.

The current deployment-oriented trace route is:

- `POST /api/v0/impact/trace-deployment-chain`

Deployment-oriented trace responses may also include:

- `deployment_facts`
- `deployment_fact_summary`

Use those when you need a stable, evidence-first contract instead of prose.

`deployment_fact_summary` reports:

- `mapping_mode`
- `overall_confidence`
- `overall_confidence_reason`
- `evidence_sources`
- fact types grouped by confidence
- `fact_thresholds`
- deployment-specific limitations such as `deployment_controller_unknown`

Mapping modes are intentionally controller-agnostic:

- `controller` for explicit controller evidence such as ArgoCD or Flux
- `iac` for explicit infrastructure-as-code evidence such as Terraform or CloudFormation
- `evidence_only` when delivery/runtime evidence exists but no trusted controller/IaC adapter was found
- `none` when no deployment evidence cleared the evidence thresholds and Eshu can only report missing inputs

That lets the same story contract work across GitOps, IaC-driven, and controller-free estates without fabricating deployment tooling.

HTTP story routes stay canonical-ID based. If the caller starts with a fuzzy
name or alias, resolve first and then call the story route. Deployment traces
start from service names because they are the operator-facing entrypoint.

### Get repository story

`GET /api/v0/repositories/{id}/story`

Example:

- `GET /api/v0/repositories/repository:r_ab12cd34/story`

### Get workload story

`GET /api/v0/workloads/{id}/story`

Examples:

- `GET /api/v0/workloads/workload:payments-api/story`
- `GET /api/v0/workloads/workload:payments-api/story?environment=prod`

### Get service story

`GET /api/v0/services/{id}/story`

Examples:

- `GET /api/v0/services/workload:payments-api/story`
- `GET /api/v0/services/workload:payments-api/story?environment=prod`

Treat the story routes as the top-level contract for repo/service/workload
narratives. Use the context routes, trace routes, and content routes named in
`drilldowns` for follow-up evidence.

For documentation generation, use this HTTP flow:

1. call a story route first
2. if it is a repository story, read `story_sections`, `deployment_overview`, `gitops_overview`, `documentation_overview`, `support_overview`, `coverage_summary`, and `limitations`
3. if it is a service story, read the dossier fields and embedded `investigation` packet directly, then call `investigate_deployment_config` for image tag, runtime setting, resource limit, values-layer, or read-first-file prompts
4. if it is a workload story, pair it with `trace_deployment_chain` before you expect deployment overviews
5. only then call content routes for exact file or snippet evidence

For cross-repo documentation or support flows, phrase the caller intent the same
way you would through MCP: tell Eshu to scan all related repositories,
deployment sources, and indexed documentation for the service or workload before asking
for the final narrative.

### Investigate deployment configuration influence

`POST /api/v0/impact/deployment-config-influence`

Use this route when the caller asks which repositories and files influence a
service's image tag, runtime settings, resource limits, values layers, or
rendered Kubernetes resources. Provide `service_name` or `workload_id`; add
`environment` to scope the answer without dropping shared values layers that do
not carry an explicit environment label.

The response is story-first and bounded per section by `limit`:

- `influencing_repositories`
- `values_layers`
- `image_tag_sources`
- `runtime_setting_sources`
- `resource_limit_sources`
- `rendered_targets`
- `read_first_files`
- `recommended_next_calls`
- `coverage.limit` and `coverage.truncated`

## Investigation API

Use this route when the caller wants Eshu to plan the repo widening and evidence
search for them instead of manually chaining story, trace, and content calls.

### Investigate a service

`GET /api/v0/investigations/services/{service_name}`

Optional query params:

- `environment`
- `intent`
- `question`

The response is investigation-first rather than story-first. Key fields:

- `repositories_considered`
- `repositories_with_evidence`
- `evidence_families_found`
- `coverage_summary`
- `investigation_findings`
- `recommended_next_calls`

Coverage fields are meant to be truthful, not optimistic:

- `complete` means the indexed repository coverage explicitly reported complete
- `partial` means the indexed context or story limitations reported partial coverage
- `unknown` means Eshu cannot currently prove complete or partial coverage from indexed evidence alone

This route is the HTTP inspection mode for operators:

- story/context routes remain the canonical-first truth surface
- investigation widens into evidence families and related repos on purpose
- inspection mode should explain gaps and widening decisions, not silently act like a second canonical graph

Use it for prompts like:

- "Explain the deployment flow for sample-service-api using Eshu only."
- "Explain the network flow for sample-service-api using Eshu only."
- "What depends on sample-service-api and what does it depend on?"

This route is designed for non-expert users who should not have to know which
deployment, GitOps, Terraform, workflow, or support repositories to inspect
next.

## Code API

Use these routes when you only need code relationships and do not need the full code-to-cloud graph.

- `POST /api/v0/code/search`
- `POST /api/v0/code/symbols/search`
- `POST /api/v0/code/topics/investigate`
- `POST /api/v0/code/relationships`
- `POST /api/v0/code/relationships/story`
- `POST /api/v0/code/dead-code`
- `POST /api/v0/code/dead-code/investigate`
- `POST /api/v0/code/complexity`

Public code-query requests accept a repository selector in the `repo_id` field
when a repository scope is part of the request. The selector may be the
canonical repository ID, repository name, repo slug, or indexed path. The
server resolves that selector to the canonical repository ID before querying.
Results should still be interpreted using canonical `repo_id + relative_path`,
not absolute server-local paths.

`POST /api/v0/code/complexity` accepts `entity_id` or `function_name` for a
single function. When neither selector is present it returns a bounded,
deterministically ordered `results` list with `limit` and `truncated`.

`POST /api/v0/code/relationships` prefers `entity_id` when the caller already
has a canonical entity. It also accepts `name` for fallback lookup, plus
optional `direction` (`incoming` or `outgoing`) and `relationship_type`
filters when the caller only needs one edge class. Set `transitive=true` with
`relationship_type=CALLS` to ask for indirect callers or callees, and use
`max_depth` to cap the traversal. Lightweight mode refuses those transitive
graph traversals with a structured `unsupported_capability` envelope instead
of returning degraded call-graph guesses.

Example transitive-caller workflow:

```json
{
  "name": "process_payment",
  "direction": "incoming",
  "relationship_type": "CALLS",
  "transitive": true,
  "max_depth": 7
}
```

Example code-only workflow:

`POST /api/v0/code/search`

```json
{
  "query": "process_payment",
  "repo_id": "payments",
  "exact": false,
  "limit": 10
}
```

Example symbol-definition workflow:

`POST /api/v0/code/symbols/search`

```json
{
  "symbol": "process_payment",
  "repo_id": "payments",
  "match_mode": "exact",
  "entity_types": ["function"],
  "limit": 25,
  "offset": 0
}
```

The symbol route returns definition-shaped results with `source_handle`,
`classification=definition`, `match_kind`, `truncated`, and `ambiguity` so MCP
callers can page or disambiguate without guessing.

Example code-topic investigation workflow:

`POST /api/v0/code/topics/investigate`

```json
{
  "topic": "repo sync authentication and GitHub App auth resolution",
  "repo_id": "eshu",
  "intent": "explain_auth_flow",
  "limit": 25,
  "offset": 0
}
```

Use this route before exact symbol lookup when the caller names a behavior
instead of one known symbol. The response includes `searched_terms`,
`matched_files`, `matched_symbols`, ranked `evidence_groups`,
`call_graph_handles`, `recommended_next_calls`, `coverage`, `limit`, `offset`,
and `truncated`. The query is content-index backed, ordered by score and stable
repo-relative path, and returns exact follow-up calls such as `get_file_lines`
or `get_code_relationship_story`.

Example relationship-story workflow:

`POST /api/v0/code/relationships/story`

```json
{
  "target": "process_payment",
  "repo_id": "payments",
  "direction": "incoming",
  "relationship_type": "CALLS",
  "limit": 25,
  "offset": 0
}
```

The relationship-story route resolves one symbol first. If the name matches
multiple entities, it returns `target_resolution.status=ambiguous` with bounded
candidates instead of querying the graph and guessing. Resolved requests use an
entity-anchored, ordered, paged relationship read and return
`coverage.truncated`, per-direction `available_by_direction`,
`returned_by_direction`, and `truncated_by_direction`, plus `source_handle`
fields. Direct reads report `max_depth=1`. Set `include_transitive=true` with
`direction=incoming` or `direction=outgoing` for bounded CALLS traversal; the
server caps `max_depth` at 10, requires `offset=0` for traversal mode, and
stops when the requested relationship window is full.

Example dead-code workflow:

`POST /api/v0/code/dead-code/investigate`

```json
{
  "repo_id": "payments",
  "language": "typescript",
  "limit": 100,
  "offset": 0,
  "exclude_decorated_with": ["@route", "@app.route"]
}
```

Use investigation mode for prompts such as "What code is dead in this repo?"
It returns `coverage`, `language_maturity`, `exactness_blockers`,
`candidate_buckets.cleanup_ready`, `candidate_buckets.ambiguous`,
`candidate_buckets.suppressed`, `source_handle`, and `recommended_next_calls`
so MCP clients do not have to infer safety from the lower-level `analysis`
object. JavaScript and TypeScript candidates stay in the ambiguous bucket until
the TypeScript/JavaScript dead-code precision corpus proves cleanup safety.

Lower-level candidate scan:

`POST /api/v0/code/dead-code`

```json
{
  "repo_id": "payments",
  "language": "c",
  "limit": 200,
  "exclude_decorated_with": ["@route", "@app.route"]
}
```

`repo_id` is optional. `language` is optional; pass it when validating one
parser language family such as `c`, `csharp`, `cpp`, `dart`, `sql`, `go`,
`groovy`, `kotlin`, `elixir`, `php`, `python`, `java`, `javascript`,
`typescript`, `tsx`, `ruby`, `rust`, or `perl`.
For C#, `csharp` is normalized to the parser language key `c_sharp`. For SQL,
the language filter narrows the
candidate scan to `SqlFunction` routines so mixed repositories with many
application functions do not starve SQL routine evidence. When `repo_id` is
omitted, the Go API returns the first page of
dead-code candidates across indexed repositories, applies the current default
Go entrypoint/test/generated exclusions plus direct Go Cobra, stdlib HTTP, and
controller-runtime framework-root signatures and Go exported public-package
roots, and uses content metadata to filter any decorator exclusions. Exported
Go symbols under `internal/`, `cmd/`, and `vendor/` remain candidates; only
public-package exports are treated as default roots. The current dead-code
response is intentionally `derived`, not `exact`, until the broader framework,
public-API, reflection, and user-configured root registry from the
reachability spec is implemented. The response body now also includes an
`analysis` object that reports the root categories currently modeled, the
specific framework-root signatures currently recognized, the per-language
dead-code maturity table, named exactness blockers for non-exact languages such
as C and Rust, observed exactness blockers carried by returned candidates, and
whether tests/generated code were excluded. Returned candidates with observed
exactness blockers classify as `ambiguous`, not cleanup-ready `unused`. C
dead-code results are `derived`: parser metadata suppresses `main`, directly
included public-header declarations, signal handlers, callback arguments, and
direct function-pointer initializer targets, while macro expansion, conditional
compilation, transitive include graphs, and dynamic symbol lookup remain named
exactness blockers. C++ dead-code results are also `derived`: parser metadata
suppresses `main`, directly included public-header declarations, virtual and
override methods, callback arguments, and direct function-pointer initializer
targets plus Node native-addon entrypoints, while broader macro expansion,
conditional compilation, transitive include graphs, template instantiation,
overload resolution, broad virtual dispatch, dynamic symbol lookup, and external
linkage remain named exactness blockers.
C# dead-code results are `derived`: parser metadata suppresses main methods,
constructors, overrides, same-file interface methods and implementations,
ASP.NET controller actions, hosted-service callbacks, test methods, and
serialization callbacks, while reflection, dependency injection, source
generators, partial type merging, dynamic dispatch, project references, and
broad public API surfaces remain named exactness blockers.
Dart dead-code results are `derived`: parser metadata suppresses top-level
`main()`, constructors and named constructors, `@override` methods, Flutter
`build` and `createState` callbacks, and public `lib/` API declarations outside
`lib/src/`, while part-file library resolution, conditional imports and
exports, package export surfaces, dynamic dispatch, Flutter route/lifecycle
wiring, generated code, reflection/mirrors, and broad public API surfaces
remain named exactness blockers.
Kotlin dead-code results are `derived`: parser metadata suppresses top-level
main functions, secondary constructors, interface methods and same-file
implementations, overrides, Gradle plugin and task callbacks, Spring component
and method callbacks, lifecycle callbacks, and JUnit methods, while reflection,
dependency injection, annotation processing, compiler plugin output, dynamic
dispatch, Gradle source-set resolution, Kotlin multiplatform target resolution,
and broad public API surfaces remain named exactness blockers.
Scala dead-code results are `derived`: parser metadata suppresses main
methods, `App` objects, traits and trait methods, same-file trait
implementations, overrides, Play controller actions, Akka actor `receive`
methods, lifecycle callbacks, JUnit methods, and ScalaTest suite classes, while
macros, implicit/given resolution, dynamic dispatch, reflection, sbt source-set
resolution, framework route files, compiler plugin output, and broad public API
surfaces remain named exactness blockers. Issue #105 dogfood validated this
contract against Play Framework and the Scala compiler with fresh `derived`
API truth after queue drain.
Elixir dead-code results are `derived`: parser metadata suppresses Application
`start/2`, public macros and guards, `@impl` behaviour callbacks,
arity-checked GenServer and Supervisor callbacks, Mix task `run/1`, protocol
functions and implementations, Phoenix controller actions shaped as `action/2`,
and arity-checked LiveView callbacks, while macro expansion, dynamic dispatch,
behaviour callback resolution, protocol dispatch, Phoenix route resolution,
supervision trees, Mix environment selection, and broad public API surfaces
remain named exactness
blockers.
Perl dead-code results are `derived`: parser metadata suppresses script
`main`, public package namespaces, Exporter-backed `@EXPORT` and `@EXPORT_OK`
functions, package constructors, `BEGIN` / `UNITCHECK` / `CHECK` / `INIT` /
`END` special blocks, `AUTOLOAD`, and `DESTROY`, while symbolic references,
AUTOLOAD target resolution, `@ISA` inheritance, Moose/Moo metadata, import side
effects, runtime `eval`, and broad public API surfaces remain named exactness
blockers.
PHP dead-code results are `derived`: parser metadata suppresses script
entrypoints, constructors, known magic methods, same-file interface methods and
implementations, trait methods, route-backed controller actions, literal route
handlers, Symfony route attributes, and WordPress hook callbacks, while dynamic
dispatch, reflection, Composer/autoload surfaces, include/require resolution,
broader framework routing, trait resolution, namespace alias breadth, magic-method
dispatch, and broad public API surfaces remain named exactness blockers.
Ruby dead-code results are `derived`: parser metadata suppresses Rails
controller actions, Rails callback methods, literal method-reference targets,
dynamic-dispatch hooks, and script entrypoints, while metaprogramming, autoload
and constant resolution, framework route files, and gem public API surfaces
remain named exactness blockers.
Groovy dead-code results are `derived_candidate_only`: parser metadata
suppresses Jenkinsfile pipeline entrypoints and Jenkins shared-library
`vars/*.groovy` `call` methods, while dynamic dispatch, closure delegates,
Jenkins shared-library resolution, and pipeline DSL dynamic steps remain named
exactness blockers.
`analysis.roots_skipped_missing_source`
counts Go candidates where the framework-root checks could not run because the
content store did not have source cached. `analysis.framework_roots_from_parser_metadata`
and `analysis.framework_roots_from_source_fallback` show whether the excluded
Go framework roots came from parser-emitted metadata or the legacy query-time
source heuristic path. `limit` defaults to `100` and is capped at `500`. The
response includes `display_truncated=true` when filtered results were clipped
to `limit`, `candidate_scan_truncated=true` when the raw graph scan reached
`candidate_scan_limit` before exclusions ran, and top-level `truncated=true`
when either condition is true.

## IaC Quality API

Use these routes when you need infrastructure-as-code cleanup candidates:

- `POST /api/v0/iac/dead`
- `POST /api/v0/iac/unmanaged-resources`

The dead-IaC route requires an explicit `repo_id` or bounded `repo_ids` scope.
When reducer-materialized reachability rows exist, the route returns those rows
with `analysis_status=materialized_reachability`; bootstrap and
`local_authoritative` graph runs materialize these rows after source-local
content projection drains. Otherwise it falls back to bounded indexed-content
analysis for Terraform modules, Helm charts, Kustomize bases/overlays, Ansible
roles/playbooks, and Docker Compose services. Used artifacts are omitted from
cleanup findings; unreferenced artifacts are returned as `candidate_dead_iac`,
and variable or template-selected artifacts are returned as
`ambiguous_dynamic_reference` when `include_ambiguous=true`.
Findings expose the canonical `repo_id` plus `repo_name` when the repository
catalog can resolve it. `findings_count` reports the number of rows returned
on the current page; `total_findings_count`, `truncated`, and `next_offset`
report whether more materialized or derived findings are available.

Example dead-IaC workflow:

```json
{
  "repo_ids": ["terraform-stack", "terraform-modules", "helm-controller", "helm-charts", "kustomize-controller", "kustomize-config", "compose-controller", "compose-app"],
  "families": ["terraform", "helm", "kustomize", "compose"],
  "include_ambiguous": true,
  "limit": 100,
  "offset": 0
}
```

The content fallback is intentionally bounded and derived. Exact cleanup
support should prefer reducer-materialized IaC usage rows so operators can
explain every finding from persisted evidence instead of broad graph anti-joins.

The unmanaged-resource route reads active AWS runtime drift reducer facts. It
requires `scope_id` or `account_id`; `region`, `finding_kinds`, `limit`, and
`offset` narrow the page. Returned findings include the AWS ARN, account,
region, `management_status`, `missing_evidence`, reducer evidence atoms, and a
recommended next action. `orphaned_cloud_resource` means Eshu saw the cloud
resource but no Terraform state or config owner. `unmanaged_cloud_resource`
means Eshu saw cloud plus Terraform state evidence but no Terraform config
owner. Raw tags remain provenance evidence and do not create environment or
ownership truth.

Example unmanaged-resource workflow:

```json
{
  "account_id": "123456789012",
  "region": "us-east-1",
  "finding_kinds": ["unmanaged_cloud_resource"],
  "limit": 100,
  "offset": 0
}
```

## Content API

Use these routes when a caller needs source text or indexed content search without relying on raw server filesystem paths.

- `POST /api/v0/content/files/read`
- `POST /api/v0/content/files/lines`
- `POST /api/v0/content/entities/read`
- `POST /api/v0/content/files/search`
- `POST /api/v0/content/entities/search`

Rules:

- portable file lookup uses `repo_id + relative_path`
- content routes accept repository selectors in `repo_id` and `repo_ids`: canonical IDs, repository names, repo slugs, or indexed paths
- portable entity lookup uses `entity_id`
- deployed API runtimes are PostgreSQL-first and PostgreSQL-only for direct content reads
- if PostgreSQL is disabled or missing a cached row, deployed HTTP reads return `source_backend=unavailable` instead of reading from a server workspace checkout
- local CLI and non-deployed helper flows may still use workspace or graph-cache fallbacks
- file and entity read responses include `source_backend` so callers can see whether the result came from `postgres`, `workspace`, `graph-cache`, or `unavailable`
- content search routes require the PostgreSQL content store and return an error payload when it is disabled
- content retrieval should not trigger `repo_access` prompting when the server already has the checkout
- documentation and runbook workflows should expect exact evidence to come from PostgreSQL-backed reads or search when available

Example file read:

```json
{
  "repo_id": "payments",
  "relative_path": "src/payments.py"
}
```

Example entity read:

```json
{
  "entity_id": "content-entity:e_ab12cd34ef56"
}
```

Example file-content search:

```json
{
  "pattern": "shared-payments-prod",
  "repo_ids": ["payments", "eshu-hq/eshu"],
  "limit": 25,
  "offset": 0
}
```

Content search is bounded at the PostgreSQL query boundary. `limit` defaults to
50 and is capped at 200. `offset` pages the same deterministic order and is
capped at 10000 so broad cold searches cannot drift into unbounded scans.
Explicit `repo_ids` searches run as one scoped query rather than one query per
repository. Responses include `limit`, `offset`, and `truncated` so MCP and
automation clients can fetch another page only when the server says one exists.

`POST /api/v0/code/cypher` is diagnostics-only. It accepts
`cypher_query` plus optional `limit` (default 100, max 1000), rejects writes,
uses a request timeout, appends a bounded `LIMIT` when the query omits one,
rejects explicit query limits above the requested cap, and returns the standard
Eshu envelope when requested. Use purpose-built code, story, impact, and content
routes for prompt contracts; raw Cypher should not be the normal fallback for
MCP answers.

## Infra API

- `POST /api/v0/infra/resources/search`
- `POST /api/v0/infra/relationships`
- `GET /api/v0/ecosystem/overview`
- `POST /api/v0/traces/resource-to-code`
- `POST /api/v0/impact/explain-dependency-path`
- `POST /api/v0/impact/change-surface`
- `POST /api/v0/impact/change-surface/investigate`
- `POST /api/v0/impact/resource-investigation`
- `POST /api/v0/environments/compare`

These routes are for tracing shared infrastructure, blast radius, dependency explanation, and environment drift.

The legacy entity-scoped impact routes `POST /api/v0/impact/blast-radius`,
`POST /api/v0/impact/change-surface`, and
`POST /api/v0/impact/trace-resource-to-code` accept `limit` with default 50 and
cap 200. They probe one extra graph row and return `truncated` so MCP and
Console callers can narrow or page instead of relying on a warm cache.

`POST /api/v0/impact/change-surface/investigate` is the prompt-facing change
surface route. It accepts one graph target family (`target` + `target_type`,
`service_name`, `workload_id`, `resource_id`, or `module_id`) and/or a code
scope (`topic`, `repo_id`, `changed_paths`). The handler first resolves the
target with exact, label-scoped graph lookups; bare `service_name` values also
probe the canonical `workload:<name>` id before falling back to name or
repo-scoped workload lookup. Generic `target` values without `target_type` try
the same bounded known-label probes instead of a whole-graph scan. Ambiguous
targets return
`target_resolution.status=ambiguous` plus candidates and do not run traversal.
Resolved targets use a typed start-node anchor, bounded traversal with
`max_depth` default 4, cap 8, `limit` default 25, cap 100, deterministic
ordering, and `truncated`. Code
topics and changed paths return `code_surface` file/symbol handles,
`recommended_next_calls`, and coverage metadata so MCP clients can answer
blast-radius and behavior-change prompts without guessing which discovery tool
to call first.

`POST /api/v0/impact/resource-investigation` is the prompt-facing
resource-first route for questions such as "what provisions this database" and
"which workloads depend on this queue." It accepts `query` or `resource_id`,
optional `resource_type` and `environment`, `max_depth` default 4 and cap 8,
and `limit` default 25 and cap 100. Ambiguous resources return
`target_resolution.status=ambiguous` plus candidates before any traversal.
Resolved resources return workload users, repository provenance paths, source
handles, limitations, recommended next calls, and `coverage.truncated`.

`POST /api/v0/compare/environments` accepts `workload_id`, `left`, `right`, and
optional `limit` with default 50 and cap 200. The comparison reads at most
`limit + 1` cloud resources per side, reports `coverage.left_truncated`,
`coverage.right_truncated`, and top-level `truncated`, and keeps the diff honest
when a workload has more dependencies than the first response includes. The
response also includes a prompt-ready `story`, `summary`, `shared`,
`dedicated`, `evidence`, `limitations`, and `recommended_next_calls` packet so
MCP callers can answer "what changed, what is shared, what is dedicated, and
what evidence backs that" without making discovery calls first. The current
contract is explicit that config and runtime-setting drift are not materialized
by this route yet; those gaps appear under `limitations`.

`POST /api/v0/infra/resources/search` accepts `query`, `category`, `kind`,
`provider`, `resource_service`, `resource_category`, and `limit`. `limit`
defaults to 50 and is capped at 200. The handler probes one extra row and
returns `truncated` so callers know when to narrow the query or fetch a
purpose-built drilldown. Terraform AWS
resource and data-source nodes preserve provider classification in both graph
and content-backed responses, so callers can narrow a search to families such
as `provider=aws`, `resource_service=s3`, or `resource_category=storage`.
Free-text `query` also checks typed resource identifiers such as Terraform
resource types and CloudFormation/SAM `resource_type` values. CloudFormation
type identifiers such as `AWS::Serverless::Function` use an exact
resource-type predicate so graph backends do not have to scan broad text fields
to answer a typed resource lookup.

Example infrastructure search:

```json
{
  "query": "aws_s3",
  "category": "terraform",
  "provider": "aws",
  "resource_service": "s3",
  "resource_category": "storage",
  "limit": 10
}
```

## Repository API

- `GET /api/v0/repositories`
- `GET /api/v0/repositories/{id}/context`
- `GET /api/v0/repositories/{id}/story`
- `GET /api/v0/repositories/{id}/stats`

Repository routes accept a repository selector in the `{id}` path segment. The
selector may be the canonical repository ID, repository name, repo slug, or
indexed path. The server resolves that selector to the canonical repository ID
before querying.

`GET /api/v0/repositories` accepts `limit` and `offset` query parameters and
returns `truncated=true` when more indexed repositories are available.

Repository responses should be treated as:

- canonical identity: `id`
- remote identity: `repo_slug`, `remote_url`
- server-local checkout metadata: `local_path`

If a downstream workflow needs local file operations on a user machine, use `repo_access` or ask the user for a local checkout path instead of assuming the server path exists locally.

For local or deployed indexing workflows, use the CLI and deployment runtime:

- local: `eshu index <path>`
- Kubernetes: repository ingestion is deployment-managed through the ingester runtime

## Ingester Status API

Use these routes to inspect the deployed ingester runtime without reaching into
Kubernetes directly.

- Canonical:
  - `GET /api/v0/status/ingesters`
  - `GET /api/v0/status/ingesters/{ingester}`
- Legacy `GET` aliases:
  - `GET /api/v0/ingesters`
  - `GET /api/v0/ingesters/{ingester}`

The default ingester is `repository`.

Status responses are designed for remote operation and include:

- ingester identity
- current status
- active run id
- last attempt / last success
- next retry timing
- repository progress counts
- failure counts and last error details

The shipped public API does not include a `POST /api/v0/ingesters/{ingester}/scan`
route in the shipped platform. Use `POST /api/v0/admin/reindex` or deployment-managed
ingestion instead of assuming a per-ingester public scan endpoint exists.

## Bundle Import API

Use this route when you want to load dependency or library internals explicitly
without indexing vendored source trees as part of the normal repository scan.

- `POST /api/v0/bundles/import`

Request contract:

- `multipart/form-data`
- file field: `bundle`
- optional form field: `clear_existing=true|false`

The route imports the uploaded `.eshu` bundle into the active graph database and
returns a success/message response describing the import result.
