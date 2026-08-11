# HTTP API Reference

The HTTP API is versioned under `/api/v0` and shares the same query model as
CLI and MCP. Use it for AI agents, automation, Console, and internal tools that
need stable JSON contracts.

This page is the map. The detailed route contracts live in focused pages so the
API reference stays readable.

## OpenAPI Source Of Truth

The live OpenAPI spec is canonical. If a narrative page and the spec disagree,
the spec wins.

- `GET /api/v0/openapi.json` - machine-readable schema
- `GET /api/v0/docs` - Swagger UI
- `GET /api/v0/redoc` - ReDoc reference

The mounted Go runtime admin OpenAPI contract lives in
`docs/openapi/runtime-admin-v1.yaml`. That contract is separate from the public
`/api/v0` schema because it describes service-local probes and admin status.

## Route Families

| Need | Start here |
| --- | --- |
| Health, readiness, index status, queue/admin controls, ingester status | [Status and admin routes](http-api/status-admin.md) |
| Capability maturity catalog (`GET /api/v0/capabilities`) | [Capability Catalog](capability-catalog.md#surfaces) |
| Surface inventory readiness (`GET /api/v0/surface-inventory`) | [Surface Inventory](surface-inventory.md#drift-gate) |
| Dashboard browser sessions, SAML SSO, and CSRF-safe Console auth | [Dashboard browser sessions](http-api/dashboard-sessions.md) |
| Component extension inventory and diagnostics | [Status and admin routes](http-api/status-admin.md#component-extension-inventory) and [Component Package Manager](component-package-manager.md) |
| Optional semantic observations and code hints | [Semantic evidence routes](http-api/semantic-evidence.md) |
| Repository-bounded semantic retrieval over curated search documents | [Semantic search route](http-api/semantic-search.md) |
| Deployment evidence, admission decisions, citations, live evidence bundle, documentation findings, packages, CI/CD, SBOM, vulnerability impact, codeowners ownership | [Evidence and supply-chain routes](http-api/evidence-and-supply-chain.md) |
| Investigation evidence packets for supply-chain impact, deployable-unit truth, and runtime drift | [Investigation Evidence Packet Contract](investigation-evidence-packet.md#http-and-mcp-surfaces) |
| Source repository to container image identity bridge | [Container image source bridge](http-api/container-image-source-bridge.md) |
| Container image (OCI) list, and bounded per-tag digest mutation history (`GET /api/v0/images`, `GET /api/v0/images/tag-history`) | [Container image, ingester, and bundle routes](http-api/images-ingesters-bundles.md) (`GET /api/v0/images`; `listContainerImageTagHistory` has no narrative sub-page yet) |
| Secrets/IAM trust chains, posture evidence, access paths, gaps, and posture summary | [Secrets/IAM routes](http-api/secrets-iam.md) |
| Entity resolution, context, incident/work-item evidence, and catalog | [Context routes and shared response contracts](http-api/context-and-stories.md) |
| Repository, workload, and service stories, intelligence reports, and investigations | [Story routes](http-api/story-routes.md) |
| Deployment-chain trace and deployment-configuration influence | [Deployment trace and influence](http-api/deployment-trace-and-influence.md) |
| Code search, symbols, relationships, call chains, dead-code, complexity, quality, language queries | [Code routes](http-api/code.md) |
| IaC cleanup, AWS drift, content reads/search, infra impact, environment comparison | [IaC, content, and infra routes](http-api/iac-content-infra.md) |
| Multi-cloud canonical resource inventory (AWS/GCP/Azure, bounded, paginated) | [Cloud inventory readback](http-api/cloud-inventory.md) |
| Natural-language answers over the graph (`POST /api/v0/ask`), agent-loop budget, answer-narration status | [Ask Eshu](http-api/ask.md) |
| Repository catalog, repository context/stats/coverage | [Repository routes](http-api/repositories-ingesters-bundles.md) |
| Ingester status, bundle search | [Container image, ingester, and bundle routes](http-api/images-ingesters-bundles.md) |

## Shared Wire Contracts

Programmatic HTTP clients should opt in to the canonical envelope with:

```http
Accept: application/eshu.envelope+json
```

Without that header, handlers may emit older payload shapes for backward
compatibility. The canonical envelope, truth levels, freshness states, cache
rules, and error-code list are owned by
[Truth Label Protocol](truth-label-protocol.md).

Runtime profile ceilings are owned by
[Capability Conformance Spec](capability-conformance-spec.md). High-authority
capabilities such as transitive call graphs, call-chain paths, dead-code
cleanup, and cross-repo impact must return `unsupported_capability` when the
active profile cannot answer correctly.

### Bounded graph-read failures

Every graph-backed route shares one bounded-availability contract rather than
collapsing a transient backend problem into HTTP 500:

| Condition | HTTP status | Error code |
| --- | --- | --- |
| Graph unavailable | `503` | `backend_unavailable` |
| Graph-read deadline expired | `504` | `backend_timeout` |

Both responses use the canonical error envelope and never expose Bolt
addresses, Cypher text, or raw driver errors. Clients should treat them as
retryable, unlike a `500`. The routes carrying this contract advertise `503`
and `504` in the OpenAPI spec; the deadline, retry, and telemetry semantics
behind it are owned by
[Graph-read safety](telemetry/graph-read-safety.md), which also records the one
route still exempt. Routes backed by Postgres or the content store rather than
the graph are unaffected.

## Shared Model Rules

- `workload` is the canonical deployable compute model.
- `service` is a convenience alias over workloads whose normalized kind is
  `service`.
- Environment-scoped calls return the logical workload plus a resolved
  `WorkloadInstance` when that evidence exists.
- Repository identity is remote-first when a git remote exists.
- Repository objects expose `repo_slug`, `remote_url`, and `local_path`.
- Repository list rows expose additive `group_*` evidence fields for
  source-backed grouping; missing evidence remains explicit.
- `local_path` is server-local metadata. It is not a portable client path.
- File-bearing results should be interpreted with `repo_id + relative_path`,
  not an absolute server path.
- `repo_access` tells a client whether it may need to ask the user for a local
  checkout path or clone decision.
- `POST /api/v0/code/import-dependencies` returns one dependency row per
  `(file, module)` pair, because that pair is the identity of the underlying
  `File-[:IMPORTS]->Module` edge. `imported_name` and `alias` are populated only
  when every import statement joining that file to that module agrees on them:
  a file importing two symbols from one module returns one row with both fields
  empty, rather than naming an arbitrary one of the two. Read an empty
  `imported_name` as "this file imports this module", not as "this file imports
  a symbol with no name".
- Path-based context routes require canonical entity IDs.
- Repository-oriented routes accept a public repository selector and normalize
  it to the canonical `repo_id` server-side.

## Authentication And Headerless Reads

A request presents its credential as `Authorization: Bearer <token>` (shared
`ESHU_API_KEY`, a scoped-token-file token, or an IdP-issued OIDC bearer token)
or, for the dashboard, a browser-session cookie. Public routes (`/health`,
`/api/v0/health`, `/api/v0/openapi.json`, the pre-auth setup/login routes, and
the rest of `publicHTTPPaths`) are always served without a credential.

Headerless requests to non-public routes are served open only when **no**
explicit credential source is configured. Configuring any one of `ESHU_API_KEY`,
`ESHU_SCOPED_TOKENS_FILE`, or `ESHU_AUTH_RESOURCE_URI` enables enforcement: a
non-public request with no `Authorization` header and no valid session cookie is
then rejected with `401`. With none of the three set the read surface is open
(the deliberate local/demo dev-mode; see
[Docker Compose](../run-locally/docker-compose.md)). Seeded bootstrap identities
and console-minted tokens are not enforcement signals on their own — close the
open read surface with one of those three environment variables. Both `cmd/api`
and `cmd/mcp-server` log the resolved posture (`auth.enforcement.configured` or
`auth.enforcement.open`) once at startup.

When `ESHU_AUTH_RESOURCE_URI` and at least one OIDC bearer provider are
configured, `cmd/mcp-server` also publishes an
[RFC 9728](https://www.rfc-editor.org/rfc/rfc9728.html) OAuth 2.0 Protected
Resource Metadata document at the unauthenticated
`/.well-known/oauth-protected-resource` route so OAuth-capable MCP clients can
discover where to obtain an access token, and adds a
`WWW-Authenticate: Bearer resource_metadata="…"` challenge to a credential-less
or unrecognized-credential `401`. A valid credential is served with no
challenge. See [MCP OAuth 2.1 Discovery](../operate/mcp-oauth-discovery.md).

## Related References

- [Truth Label Protocol](truth-label-protocol.md)
- [Capability Conformance Spec](capability-conformance-spec.md)
- [Runtime Admin API](runtime-admin-api.md)
- [Local Testing](local-testing.md)
