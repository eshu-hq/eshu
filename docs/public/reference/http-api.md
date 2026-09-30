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
| Optional semantic observations and code hints, bound to active generations by default | [Semantic evidence routes](http-api/semantic-evidence.md) |
| Repository-bounded semantic retrieval over curated search documents | [Semantic search route](http-api/semantic-search.md) |
| Deployment evidence, admission decisions, citations, live evidence bundle, documentation findings, packages, CI/CD, SBOM, vulnerability impact with read-time runtime corroboration, codeowners ownership | [Evidence and supply-chain routes](http-api/evidence-and-supply-chain.md) |
| Coverage and uncertainty in the bounded CI/CD static workflow scan | [CI/CD workflow coverage](http-api/workflow-coverage.md) |
| Investigation evidence packets for supply-chain impact, deployable-unit truth, and runtime drift | [Investigation Evidence Packet Contract](investigation-evidence-packet.md#http-and-mcp-surfaces) |
| Source repository to container image identity bridge | [Container image source bridge](http-api/container-image-source-bridge.md) |
| Container image (OCI) list, and bounded per-tag digest mutation history (`GET /api/v0/images`, `GET /api/v0/images/tag-history`) | [Container image, ingester, and bundle routes](http-api/images-ingesters-bundles.md) (`GET /api/v0/images`; `listContainerImageTagHistory` has no narrative sub-page yet); `GET /api/v0/images/tag-history` binds a scoped token to its repository grant through `ContainerImage-[:BUILT_FROM]->Repository` (#6564): a row is kept only when the image at its `resolved_digest` is `BUILT_FROM` a granted repository, `previous_digest` is omitted unless its image is too, and observations whose image has no `BUILT_FROM` edge are withheld, so a scoped caller can see less history than a shared-key caller. `mutated` is left as observed, so a row with `mutated: true` and no `previous_digest` still discloses that some prior digest existed, without disclosing which. A grant-filtered page is refilled across further reads until it holds `limit` visible rows, the history ends, or a small per-request read cap is reached, so on a filled page `count` below `limit` does not measure withheld rows; each refill read covers a fixed 200 raw rows whatever `limit` was asked for, so one request scans a constant 800. A scan that read only withheld rows returns `count: 0` with `truncated: true` rather than ending the history. `next_cursor` is an authenticated-encryption envelope over a row key, bound to the `image_ref` it was issued for; its plaintext names one row's `first_observed_at` and `uid` rather than a row position, so paging is forward-only (an observation inserted before that point is not returned on a later page) and the token stays valid across a `limit` change mid-walk. Pass it back as `cursor` exactly as issued and keep following it until `truncated` is false; an edited, synthesized, foreign-`image_ref` or pre-rotation token gets a 400, so restart from page one. The envelope is bound to the repository grants it was issued under as well, so another caller's token does not open — and neither does your own once your grants change mid-walk, which also means restarting from page one. What a cap-reached page discloses is a **count and never an identity**: with `truncated: true` and `count` below `limit`, the remainder of the 800 raw observations after the row the caller was last shown — or after the frontier of its previous capped page — are ones it may not see. It cannot choose where that span starts, cannot read the frontier, and never learns a withheld observation's `first_observed_at`, `uid` or digest. Sealing uses the deployment DEK (`ESHU_AUTH_SECRET_ENC_KEY` or `ESHU_AUTH_SECRET_ENC_KEY_FILE`); where none is configured, a grant-filtered truncated page omits `next_cursor` and says so in `truth.reason`, a grant-filtered request carrying a `cursor` is refused with a 503 `capability_degraded`, and unscoped callers are unaffected. `offset` remains for unscoped and all-scope callers; a grant-filtered caller sending a non-zero `offset` gets a 400 and its response omits the `offset` field. Scoped responses carry `grant_filtered: true` |
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

Repository context marks unavailable content coverage and bounded or failed
file-derived overviews in `partial_reasons`; see
[Repository routes](http-api/repositories-ingesters-bundles.md).

Saved IdP group mapping refs from before the FIPS-safe SHA-256 transition must
be refreshed through the admin mapping list before deletion; a legacy ref
returns `409` (see [Dashboard browser sessions](http-api/dashboard-sessions.md)).

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
route still exempt. The graph-read contract does not apply to Postgres or
content-store reads, except the reader fence failures below; those follow the
store-error contract below.

Any other graph-read failure (a driver fault that is neither a deadline nor an
availability problem) answers `500` whose detail ends in `graph query failed`;
a handler may prefix it with its own step name, for example `query k8s
resources: graph query failed`. The driver's own message quotes the statement,
inline literals included, so it never reaches a response body. The redacted
detail is in the `query.graph_read.error` log and on the `neo4j.query` span.
The response carries no trace id and no statement fingerprint, so an operator
matches a reported failure to its log record by the time of the failure, and
reads the query name, statement fingerprint, and statement head from the record
to name the offending shape.

#### PostgreSQL reader fence failures

When the API or MCP server reads through a read replica (see
[PostgreSQL Read Routing](../deployment/postgres-read-routing.md)), a read can
fail closed because the replica has not replayed to the writer checkpoint within
the replay window, or because acquiring or checking a reader connection (the
pool wait, the connection dial, or the identity check) hit the same deadline.
Both are timeouts and transient.
The dead-code routes (`POST /api/v0/code/dead-code`, `.../cross-repo`,
`.../investigate`), `POST /api/v0/iac/dead`,
`GET /api/v0/supply-chain/impact/findings` (its findings read and cloud-runtime
probe read, #7548, as well as its kubernetes-runtime and runtime-context
probes), and every other route that writes its failures through the shared
graph-read helper answer:

| Condition | HTTP status | Error code | Message |
| --- | --- | --- | --- |
| Replica behind the writer checkpoint, or reader connection acquisition or identity check timed out | `503` | `backend_unavailable` | `database read temporarily unavailable; retry shortly` |

The response carries `Retry-After: 2` (a fixed number of seconds, sized to the
2-second replay window) and the same hint in the envelope as
`error.details.retry_after_seconds`, because the MCP transport forwards the
envelope rather than HTTP headers. The body never carries the Go error text.
A failed request-level checkpoint step (the writer checkpoint query erroring or
timing out, or the writer failing its topology check; replay lag is a reader-fence
condition, not a checkpoint-step one) answers the same `503` and `Retry-After`; a checkpoint source that was never
configured is a wiring state and carries no hint.
Clients should retry after the hinted delay. `Retry-After` is set only on these
transient verdicts, on the graph-unavailable `503` above, and on the
identity-store `503` described below; any other `503`
`backend_unavailable`, such as route-to-caller tracing on a deployment with no
graph backend configured, is a configuration state and carries no hint. The
replay and acquisition budget is derived from the request's own context, so a
request whose own deadline expires during a reader borrow, identity check, or
replay now answers this retryable `503` (with `outcome="deadline"` on the
reader stage histogram) where it previously answered `500` (or `504` on a route
that classified the error with the bounded-read classifier). A reader failure that is neither
condition stays a `500` with a fixed message and no `Retry-After`: a rejected
statement, a reader connection that fails to authenticate or connect (refused
connection, TLS error), a role denied `pg_control_system()` or another identity
or replay query, and a client disconnect are not transient, so the API does not
tell the client to retry them. Every supply-chain query route sends its store
reads through the shared helper first (#7549): a stale or timed-out guarded
reader answers the retryable `503` with `Retry-After` above, while any other
store failure stays a handler-owned `500` with a
`supply_chain_query.stage_failed` log line. Routes outside the dead-code and
dead-IaC lanes and `GET /api/v0/supply-chain/impact/findings` that write a
store error straight into a `500` do not yet map a reader fence failure and
still answer `500` until they are routed through the shared helper.

A bearer credential is checked against the PostgreSQL identity store. When that
store cannot answer (a lost or refused connection, a refusal for lack of
resources such as too many connections, or a statement timeout), the
credential is not judged either way, so the API does not answer `401`. It
answers `503` `backend_unavailable` with `Retry-After: 2`, the fixed message
`identity store temporarily unavailable; retry shortly`, and no
`WWW-Authenticate` challenge, and it records a governance-audit decision of
`unavailable` with reason `identity_store_unavailable` instead of a denial. The
handler never runs, so an unevaluated credential is never admitted. Every other
resolver failure, such as a rejected statement or a client disconnect, still
answers a bare `401`. An operator sees the failure on the
`auth.identity_store.unavailable` log event and the
`eshu_dp_auth_identity_store_unavailable_total` counter, labeled by
`failure_class`: `unavailable` and `timeout` are transient, and `topology` means
the writer was refused because the primary it was bootstrapped against was
replaced (a promotion or a restore) or the dial reached a different cluster. That
answer still carries `Retry-After`, as the checkpoint step does, but a retry will
not help until the API process restarts or the database target is corrected.

A failed Postgres call on the API and MCP server answers `500`, `503`, or `504`
per the handler, and its detail never carries the driver's own message, which
names the database user, the database, and the dialed address on a connection
failure and a relation, column, constraint, or bound value on a server error. A
business read through the guarded reader pool ends in a fixed text such as
`PostgreSQL reader query failed` (#7482). A call through the writer pool
(authorization, audit, mutation, sign-in, and the admin, recovery, and supply-chain
routes that use it) ends in one of four fixed texts:
`postgres store unavailable` (a connection that could not be made or was lost),
`postgres store timed out` (a statement or transaction ran out of time),
`postgres store request canceled` (the caller went away), or
`postgres store statement failed` (any other driver or server failure). A handler
may prefix its own step name, for example
`<step>: postgres store statement failed`. When the writer is down, a business read
answers `PostgreSQL writer checkpoint failed`, because the request's freshness
checkpoint runs on the writer first. The writer pool's detail is
in the `postgres.store.error` log (`postgres_store.operation`,
`postgres_store.sqlstate`, `postgres_store.statement_head`,
`postgres_store.error`). The ingester, reducer, projector, collectors, and
`admin-status` pools are not bounded and still return the driver's text on their
own admin endpoints.

The two routes that run a caller-authored statement, `POST /api/v0/code/cypher`
(`execute_cypher_query`) and `POST /api/v0/code/visualize`, treat a statement
the graph rejects as malformed (`Neo.ClientError.Statement.*`) as the caller's
fault: they answer `400 invalid_argument` with the first line of the graph's
message, every numeric and string literal replaced by `<REDACTED>`, so the
author can fix the query. The quoted copy of the statement that follows the
message is not returned. Any other failure on those routes is the same `500` as
above. Because the redaction reads the message as Cypher, the offending token
and the numbers in a `line 1, column 24` position are replaced too.

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
- Content entity search uses stable `(repo_id, relative_path, start_line, entity_id)` pagination and reports `truncated` when another matching row follows.
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

### All-scope credentials on grant-filtered routes

A credential marked all-scope — a `ESHU_SCOPED_TOKENS_FILE` entry with
`all_scopes`, an OIDC bearer resolved through an admin group grant, or an
owner console session — carries no repository or scope ids. On a route whose
handler intersects its read with those ids there is nothing to intersect, so
the read would run across every tenant. `ESHU_GOVERNANCE_MODE` decides what
happens instead: `hosted_multi_tenant` and any unrecognized value refuse such a
caller with `403 permission_denied` and record
`scoped_route_all_scope_grant_required`; `local_no_policy`,
`hosted_single_tenant`, and an unset mode admit it when it is bound to one
tenant and workspace, which is the intended posture where one graph belongs to
one tenant. Identity and admin routes under `/api/v0/auth/`, and the static
catalog and request-reshape routes, hold no tenant data to filter and admit it
in every mode. Credentials carrying real ids are unaffected in every mode.
Every operation that can refuse a caller this way declares `403` in the OpenAPI
document, so a generated client has a case for it without deploying under
`hosted_multi_tenant` to discover the status.

No route currently refuses a scoped caller in every mode for lack of a grant
binding. `pendingRowFilteringRoutes` in the Go source (#5167) is the ledger for
such a route: one listed there declares `403`, states the reason in its OpenAPI
description, and repeats it in its MCP tool description. The last entry,
`GET /api/v0/freshness/services/changed-since`, left it when #6475 put the
writing ingestion scope on every service lineage row, so the route now binds
the caller's grant in SQL like the other freshness reads. Details are in the
[service changed-since reference](http-api/service-changed-since.md).

`GET /api/v0/status/index` and its legacy alias `GET /api/v0/index-status` are
grant-filtered routes of this kind (#5167). A restricted scoped caller does not
get the deployment-wide report; it gets a `repository_count` counted over its
granted repositories inside the graph query, plus a `withheld_sections` list
naming what it does not receive. See
[Index Status](http-api/index-status.md) for the exact shape. An
all-scope caller has no grant to bind there and follows the mode rule above.

The rule reaches bearer tokens and browser sessions alike, with one difference:
it never widens a token's reach. A route absent from the scoped-token allowlist
refuses every bearer in every mode, while the modes above do admit an
owner console session there.
On the MCP transport — `mcp-server`'s `GET /sse` and `POST /mcp/message` — the
refusal lands on the handshake, so an all-scope bearer loses the whole MCP
session rather than the tools that read tenant data. Only bearers reach that
rule: the transport is wired with no browser-session resolver, so a console
session cookie is not a credential there at all. See
[Hosted Governance](../operate/hosted-governance.md).

When `ESHU_AUTH_RESOURCE_URI` and at least one OIDC bearer provider are
configured, `cmd/mcp-server` also publishes an
[RFC 9728](https://www.rfc-editor.org/rfc/rfc9728.html) OAuth 2.0 Protected
Resource Metadata document at the unauthenticated
`/.well-known/oauth-protected-resource` route so OAuth-capable MCP clients can
discover where to obtain an access token, and adds a
`WWW-Authenticate: Bearer resource_metadata="…"` challenge to a credential-less
or unrecognized-credential `401`. A valid credential is served with no
challenge. See [MCP OAuth 2.1 Discovery](../operate/mcp-oauth-discovery.md).

### Scoped callers on the impact path routes

`POST /api/v0/impact/trace-resource-to-code`,
`POST /api/v0/impact/explain-dependency-path`, and
`POST /api/v0/impact/trace-exposure-path` walk through nodes that carry no
`repo_id`, so a scoped caller's grant is applied node by node over the bounded
page rather than as one query predicate (#5167):

- A `Repository` is owned when its id is granted. A `Workload`,
  `WorkloadInstance`, `TerraformResource`, `TerraformModule`,
  `KubernetesWorkload`, `Function`, `SqlTable`, or `ShellCommand` is owned when
  its `repo_id` is granted; a `WorkloadInstance` is also owned when it has a
  `DEPLOYMENT_SOURCE` edge to a granted repository. A `CloudResource` is owned
  when a granted `WorkloadInstance` `USES` it, and a `TerraformStateResource`
  when a granted `TerraformResource` `MATCHES_STATE` it. Every other class
  (`Platform`, `Endpoint`, `CloudAction`, `EvidenceArtifact`, `TerraformOutput`,
  `DataAsset`, `CidrBlock`, `SecretsIAMSecretMetadataPath`, and a
  `CloudResource` no granted instance uses) is not owned.
- A path that crosses any node the grant does not own is dropped whole, never
  shortened or redacted.
- An ungranted start, source, or endpoint renders exactly as an unknown one and
  runs no traversal. An empty grant returns the empty answer without a graph
  read.
- A start, source, or endpoint given by name resolves, for a scoped caller, to
  the first node carrying that id or name that the grant owns, in id order
  among up to 32 matches. A name another tenant also uses therefore cannot
  hide the caller's own node, and a name only other tenants use still renders
  as unknown. Unscoped callers keep the first match.
- `truncated` (`coverage.truncated` on the exposure route) is computed from the
  raw row count before the filter, so a scoped page can hold fewer than `limit`
  paths. It is also true when the page held more nodes to check than the
  per-request ownership budget allows: 4500 distinct statement-checked keys,
  whatever the grant size. Nodes past that budget count as not owned, and so
  does a widely shared `CloudResource`, `WorkloadInstance`, or
  `TerraformStateResource` whose granted owner falls past the 800 owner rows
  read per 50-node chunk.
- Grants are matched on repository ids. A token whose grant holds only
  ingestion scope ids, and no repository id, sees empty answers on these three
  routes.
- The exposure route always withholds `SecretsIAMSecretMetadataPath` and
  `CidrBlock` sinks from scoped callers and names them in
  `coverage.unresolved_reason`. The scoped exposure filter is proven end to
  end on Neo4j.
- Every scoped response carries `scoped: true` and a static
  `withheld_sections` list. Both are present whether or not anything was
  withheld.

`explain-dependency-path` returns `400` immediately when a shared caller sends
identical `source` and `target` arguments. A scoped caller first checks that the
endpoint is visible; an unknown or ungranted endpoint returns `404`. Distinct
arguments that resolve to the same entity, such as its name and ID, return
`400` after resolution.

## Dashboard Browser Sessions

Moved. See [Dashboard browser sessions](http-api/dashboard-sessions.md) for the
Console browser flow, SAML SSO, CSRF rules, and the local identity routes.

## Ask Eshu — POST /api/v0/ask

Moved. See [Ask Eshu](http-api/ask.md) for the request and response contract,
the SSE variant, and the agent loop budget.

## Answer-narration status seam — hot-path evidence (issue #3263 follow-up)

Moved. See
[the answer-narration status seam](http-api/ask.md#answer-narration-status-seam-hot-path-evidence-issue-3263-follow-up).

## Cloud Inventory Readback

Moved. See
[cloud inventory readback](http-api/cloud-inventory.md#cloud-inventory-readback).

## Cloud Resource Graph Paging

Moved. See
[cloud resource graph paging](http-api/cloud-inventory.md#cloud-resource-graph-paging).

## Related References

- [Truth Label Protocol](truth-label-protocol.md)
- [Capability Conformance Spec](capability-conformance-spec.md)
- [Runtime Admin API](runtime-admin-api.md)
- [Local Testing](local-testing.md)
