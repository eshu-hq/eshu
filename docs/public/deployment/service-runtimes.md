# Service Runtimes

Use this page when you need the deployment ownership map: which Eshu runtimes
exist, which service owns a problem, and which focused page has the detail.

For Kubernetes install steps, use
[Helm Quickstart](../deploy/kubernetes/helm-quickstart.md). For local stack
choices, use [Docker Compose](../run-locally/docker-compose.md).

## Runtime Map

| Runtime | Owns | Kubernetes shape | Detail |
| --- | --- | --- | --- |
| Schema Bootstrap | Postgres and graph schema DDL only. | `Job` | [Bootstrap runtimes](service-runtimes-bootstrap.md) |
| API | HTTP API, query reads, and admin endpoints. | `Deployment` | [Core runtimes](service-runtimes-core.md) |
| MCP Server | MCP HTTP/SSE or stdio transport over the query surface. | optional `Deployment` | [Core runtimes](service-runtimes-core.md) |
| Ingester | Repository sync, workspace PVC, parsing, fact emission. | `StatefulSet` | [Core runtimes](service-runtimes-core.md) |
| Webhook Listener | Verified Git and AWS freshness trigger intake. | optional `Deployment` | [Core runtimes](service-runtimes-core.md) |
| Workflow Coordinator | Collector instance reconciliation, claim creation, expired-claim reaping. | optional `Deployment` | [Core runtimes](service-runtimes-core.md) |
| Resolution Engine | Durable queue drain, projection, retry, replay, recovery, and bounded superseded-generation cleanup. | `Deployment` | [Resolution engine](../services/resolution-engine.md) |
| Hosted Collectors | Confluence, OCI registry, Terraform-state, AWS cloud, package-registry, SBOM, attestation, provider security-alert, PagerDuty incident-context, and Jira work-item fact intake. | optional `Deployment` | [Collector runtimes](service-runtimes-collectors.md) |
| Scanner Worker | Claim-driven CPU-heavy and memory-heavy security analyzers that emit source facts only. | optional `Deployment` | [Security Intelligence](../reference/security-intelligence.md#scanner-worker-boundary) |
| Bootstrap Index | One-shot initial indexing. | operator-run helper, not chart steady state | [Bootstrap runtimes](service-runtimes-bootstrap.md) |

The direct service binaries are the release artifacts and support `--version`
checks. Helm starts API and MCP through the `eshu` CLI wrapper; most other
workloads use direct `/usr/local/bin/eshu-*` binaries.

Deployment binaries connect to external Bolt-compatible graph endpoints.
Embedded NornicDB is only the local `eshu graph start` path.

## Health Versus Completeness

Long-running HTTP runtimes expose the shared admin surface:

- `/healthz` for process health
- `/readyz` for dependency readiness
- `/metrics` for Prometheus signals
- `/admin/status` for runtime backlog, generation, and failure state
- `/admin/replay`, `/admin/refinalize`, and
  `/admin/replay-collector-generations` only on runtimes configured with the
  recovery handler

A green pod is not proof that indexing finished. Use the completeness routes
before treating a graph as current:

- `GET /api/v0/status/index`
- `GET /api/v0/index-status`
- `GET /api/v0/repositories/{repo_id}/coverage`

`bootstrap-index` is one-shot and does not mount the shared HTTP admin surface.

## MCP HTTP Transport Auth (Breaking Change)

Issue #5168 addressed a gap where `initialize`, `tools/list`, `ping`, and
`GET /sse` session establishment on the MCP HTTP transport were reachable
without going through any credential check — only `tools/call` was checked, and
only incidentally, through its internal re-dispatch. `GET /sse` and
`POST /mcp/message` (every JSON-RPC method) now pass through the same credential
middleware as `/api/v0/*`, backed by the same chain: `ESHU_API_KEY`, a scoped
token from `ESHU_SCOPED_TOKENS_FILE`, or an IdP-issued bearer token accepted by
`ESHU_AUTH_RESOURCE_URI`. An SSE session is also bound to the credential that
opened it — a different credential cannot post to that session's `sessionId`
(rejected with `403` before the body is decoded).

What #5168 gives you:

1. **Transport wrap.** The MCP transport routes run through the credential
   middleware instead of being mounted with none, so a presented credential is
   validated the same way the query API validates it, and SSE sessions are
   principal-bound.
2. **No-silent-open startup gate.** `ESHU_MCP_TRANSPORT=http` with no resolvable
   credential source refuses to start with an actionable error, unless
   `ESHU_MCP_ALLOW_UNAUTHENTICATED=true` is set.
3. **Fail-closed for shared-key deployments.** When `ESHU_API_KEY` is set, a
   headerless request is refused with a bare `401` that discloses no tool
   catalog or server identity.

**Residual gap for scoped-token-only and OIDC-only deployments (tracked,
closed by the companion fix).** #5168 does not by itself make every *request*
authenticated when the shared `ESHU_API_KEY` is unset and only
`ESHU_SCOPED_TOKENS_FILE` or `ESHU_AUTH_RESOURCE_URI` is configured. In that
configuration the shared credential middleware still lets a *headerless* request
through — its dev-mode bypass keys off an empty shared token alone, regardless
of a configured scoped or OIDC resolver — so `GET /sse`, `POST /mcp/message`,
and the internally re-dispatched `tools/call` remain reachable without a
credential. The #5168 startup gate treats a configured scoped-token file or
OIDC resolver as a credential source, so such a deployment starts; it is the
per-request headerless enforcement that is finished by the companion
auth-headerless-bypass hardening (under #5161), the immediately-following change
in this auth chain. Until that lands, run scoped-only or OIDC-only MCP HTTP
deployments behind a network boundary, or also set `ESHU_API_KEY`.

**Breaking change for operators running a keyless HTTP deployment.** Before
#5168, `ESHU_MCP_TRANSPORT=http` with no credential source served the MCP
transport openly (only the query API surface behind it was gated). Now, that
configuration refuses to start with an actionable error. Configure one of the
three credential sources above, or set
`ESHU_MCP_ALLOW_UNAUTHENTICATED=true` to keep the previous open behavior for
loopback/dev use only — never on a publicly reachable port. `stdio`
(`eshu mcp start`, `eshu local-host mcp-stdio`) is unaffected: it keeps its
process/filesystem trust boundary and is never gated by credential
configuration.

## Operational Defaults

- Keep the workspace PVC on the ingester only.
- Scale API and MCP for read traffic.
- Scale the resolution engine only after queue and Postgres telemetry show the
  reducer is the bottleneck.
- Keep `ESHU_REPO_DEPENDENCY_PROJECTION_WORKERS` at the backend-aware default:
  `4` for the proven NornicDB path and `1` for Neo4j compatibility. Set `1` or
  `2` on NornicDB only for a measured resource constraint. Other values fall
  back to the backend default. Fixed acceptance-unit
  sharding serializes the complete retract-then-rewrite cycle for the same
  source repository while allowing unrelated repositories to run concurrently.
  Each process appends its hostname, PID, and a boot-unique nonce to the
  configured lease-owner prefix. The default `5m` lease must exceed the `45s`
  whole-cycle deadline plus `ESHU_CANONICAL_WRITE_TIMEOUT` and a `30s` margin.
  Failed, canceled, or ambiguous cycles retain the shard lease until expiry;
  independent shards continue to run.
- `ESHU_REPO_DEPENDENCY_RETRACT_STATEMENT_TIMING` is retained for
  compatibility but no longer changes behavior: repo-dependency retract
  statements always run sequentially with per-statement timing logs (grouped
  DELETEs under-apply on NornicDB v1.1.11).
- Keep claim-driven collectors behind an active workflow coordinator.
- Keep scanner workers in their own Deployment or Compose service; do not move
  image unpacking, SBOM generation, source scanning, secret scanning, license
  scanning, OS package extraction, or misconfiguration analysis into the
  default reducer lane.
- Use `ServiceMonitor` only for long-running Kubernetes runtimes; schema
  bootstrap and bootstrap-index are excluded.
- Enable `ESHU_PPROF_ADDR` only on the runtime that owns the slow stage and keep
  it private.
- Keep `ESHU_REDUCER_HANDLES_ROUTE_PRESENCE_GATE_ENABLED` at its default (`true`)
  so the symbol→runtime edges (`Function-[:HANDLES_ROUTE]->Endpoint` and
  `Function-[:RUNS_IN]->Workload`) cannot drop on a cold first generation; it is
  independent of the secrets/IAM projection flag. See
  [Resolution engine](../services/resolution-engine.md) for the kill switch.
- The resolution engine bounds canonical and semantic graph writes with two
  independent permit pools (`ESHU_GRAPH_WRITE_CANONICAL_MAX_IN_FLIGHT`,
  `ESHU_GRAPH_WRITE_SEMANTIC_MAX_IN_FLIGHT`) so a slow write on one class
  cannot starve the other. Leave both unset to keep the legacy
  `ESHU_GRAPH_WRITE_MAX_IN_FLIGHT` behavior: while neither per-class var is
  set, an aggregate gate bounds the COMBINED canonical+semantic total to that
  legacy ceiling rather than doubling it. Set either per-class var only after
  evidence shows one class needs a different ceiling than the other; see
  `go/cmd/reducer/README.md` "Graph write backpressure" for the full knob
  table.
- The reducer always publishes the poison dead-letter stuck-gauge
  (`eshu_dp_poison_dead_letter_scopes`); a non-zero value means a scope has
  permanently wedged (`dead_letter` work items with no newer generation). The
  bounded auto-retry arm that re-drives them is **off by default**
  (`ESHU_POISON_LIVENESS_AUTO_RETRY_ENABLED=false`, surface-only); enable it
  only after an operator has reviewed the wedge, and tune the poll/attempt/batch
  budget via the `ESHU_POISON_LIVENESS_*` knobs documented in
  `go/cmd/reducer/README.md` "Poison dead-letter liveness".

## Route Map

| Need | Use |
| --- | --- |
| Commands, templates, and scaling notes for core services | [Core Runtime Services](service-runtimes-core.md) |
| Collector control-plane and hosted collector matrix | [Collector Runtime Services](service-runtimes-collectors.md) |
| Schema bootstrap and bootstrap-index contract | [Bootstrap Runtime Services](service-runtimes-bootstrap.md) |
| Helm values and render-time guards | [Helm Values](../deploy/kubernetes/helm-values.md) |
| Metrics, traces, logs, and status signal names | [Telemetry Overview](../reference/telemetry/index.md) |
