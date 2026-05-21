# HTTP Context And Story Routes

Use these routes when the caller has a fuzzy name or canonical entity and needs
context, catalog navigation, a narrative story, or an investigation packet.

## Entity Resolution

`POST /api/v0/entities/resolve`

Use this before context lookups when the caller has a fuzzy name, alias, or
partial resource description.

```json
{
  "name": "payments-api",
  "type": "workload",
  "repo_id": "payments",
  "limit": 5
}
```

Responses include `entities`, `count`, normalized `limit`, and `truncated` so
clients can page or disambiguate before calling context routes.

## Context Routes

- `GET /api/v0/entities/{entity_id}/context`
- `GET /api/v0/workloads/{workload_id}/context`
- `GET /api/v0/services/{service_name}/context`
- `GET /api/v0/repositories/{repo_id}/context`

Examples:

- `GET /api/v0/entities/workload:payments-api/context`
- `GET /api/v0/workloads/workload:payments-api/context?environment=prod`
- `GET /api/v0/services/workload:payments-api/context?environment=prod`

`/services/{service_name}/context` is an alias route over workload context and
adds `requested_as=service`.

When a repository has workload identity facts but no materialized `Workload`
node yet, service context can fall back to the repository read model. Those
responses use `materialization_status=identity_only`,
`query_basis=repository_read_model`, an empty `instances` array, and a
`limitations` entry of `workload_identity_not_materialized`.

Entity context responses may include semantic narrative fields when the entity
carries normalized semantic metadata:

- `semantic_summary`
- `semantic_profile`
- `story`

## Catalog

`GET /api/v0/catalog`

The catalog route is the bounded navigation surface for Console and MCP
clients. It returns handles for:

- indexed repositories
- canonical `Workload` graph nodes when a graph backend is available
- identity-only workloads from the repository read model
- services, which are workload rows whose normalized `kind` is `service`
- counts, `limit`, and `truncated`
- limitations when the runtime can only return repository handles

The optional `limit` query parameter caps each returned collection. The default
is `2000`; the maximum accepted value is `5000`.

No-Regression Evidence: The catalog route uses bounded repository, workload
graph, and workload-identity reads with `LIMIT`; it returns handles rather than
story payloads.

No-Observability-Change: Graph reads continue through the query package
`GraphQuery` port and existing handler error paths.

## Story Routes

Use story routes when the caller wants a structured narrative first and
evidence second.

- `GET /api/v0/repositories/{repo_id}/story`
- `GET /api/v0/workloads/{workload_id}/story`
- `GET /api/v0/services/{service_name}/story`
- `POST /api/v0/impact/trace-deployment-chain`

Repository story responses may include:

- `subject`
- `story`
- `story_sections`
- `semantic_overview`
- `deployment_overview`
- `gitops_overview`
- `documentation_overview`
- `support_overview`
- `coverage_summary`
- `limitations`
- `drilldowns`

Service story responses expose the one-call service dossier contract:

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

Deployment trace responses are evidence-first and may include deployment,
GitOps, controller, runtime, cloud, Kubernetes, image, relationship, and
fact-summary sections.

Mapping modes in deployment traces are controller-agnostic:

- `controller` for explicit controller evidence such as ArgoCD or Flux
- `iac` for explicit infrastructure-as-code evidence such as Terraform or
  CloudFormation
- `evidence_only` when delivery/runtime evidence exists but no trusted
  controller or IaC adapter was found
- `none` when no deployment evidence cleared the thresholds

HTTP story routes stay canonical-ID based. If a caller starts with a fuzzy
name or alias, resolve first and then call the story route.

## Service Story Disambiguation

`GET /api/v0/services/{service_name}/story`

Optional query params:

- `service_id` is the exact workload/service ID selector.
- `repo` disambiguates duplicate service names by canonical repository ID,
  name, slug, path, or remote URL.
- `environment` narrows duplicate service names to workloads with a matching
  runtime instance environment.

When the name resolves to multiple workloads, the route returns HTTP 409 with
canonical envelope `error.code=ambiguous`, `data=null`, and candidate details.
It does not pick the first matching workload.

## Documentation Generation Flow

For documentation generation:

1. Call a story route first.
2. For repository stories, read `story_sections`, deployment, GitOps,
   documentation, support, coverage, and limitation fields.
3. For service stories, read the dossier fields and embedded investigation
   packet.
4. Pair workload stories with `trace_deployment_chain` before expecting rich
   deployment overviews.
5. Call content routes only when exact file or snippet evidence is needed.

## Deployment Config Influence

`POST /api/v0/impact/deployment-config-influence`

Use this route when the caller asks which repositories and files influence a
service image tag, runtime setting, resource limit, values layer, or rendered
Kubernetes resource. Provide `service_name` or `workload_id`; add
`environment` to scope the answer without dropping shared values layers.

The response is story-first and bounded per section by `limit`.

## Service Investigation

`GET /api/v0/investigations/services/{service_name}`

Optional query params:

- `environment`
- `intent`
- `question`

The response is investigation-first rather than story-first. It includes
repositories considered, repositories with evidence, evidence families found,
coverage summary, findings, and recommended next calls.

Use it for prompts where the operator should not have to know which deployment,
GitOps, Terraform, workflow, or support repositories to inspect next.
