# Service Runtimes

Use this page when you need the deployment ownership map: which Eshu runtimes
exist, which service owns a problem, and which focused page has the detail.

For Kubernetes install steps, use
[Helm Quickstart](https://github.com/eshu-hq/eshu-deploy/blob/main/docs/kubernetes/helm-quickstart.md). For local stack
choices, use [Docker Compose](https://github.com/eshu-hq/eshu-deploy/blob/main/docs/run-locally/docker-compose.md).

## Runtime Map

| Runtime | Owns | Kubernetes shape | Detail |
| --- | --- | --- | --- |
| Schema Bootstrap | Postgres and graph schema DDL only. | `Job` | [Bootstrap runtimes](service-runtimes-bootstrap.md) |
| API | HTTP query/admin reads and the all-scopes vulnerability-suppression policy mutation. | `Deployment` | [Core runtimes](service-runtimes-core.md) |
| MCP Server | MCP HTTP/SSE or stdio transport over the query surface. | optional `Deployment` | [Core runtimes](service-runtimes-core.md) |
| Ingester | Repository sync, workspace PVC, parsing, fact emission. | `StatefulSet` | [Core runtimes](service-runtimes-core.md) |
| Webhook Listener | Verified Git and AWS freshness trigger intake. | optional `Deployment` | [Core runtimes](service-runtimes-core.md) |
| Webhook Intake Host | Authenticated satellite trigger admission and trigger-table schema ownership. | optional `Deployment` (ClusterIP only, no ingress) | [Core runtimes](service-runtimes-core.md) |
| Workflow Coordinator | Collector instance reconciliation, claim creation, expired-claim reaping. | optional `Deployment` | [Core runtimes](service-runtimes-core.md) |
| Resolution Engine | Durable queue drain, projection, retry, replay, recovery, bounded superseded-generation cleanup, package-consumption sidecar repair, and hardcoded-secret side-table rebuild (#8833). | `Deployment` | [Resolution engine](../services/resolution-engine.md) |
| Hosted Collectors | Confluence, OCI registry, Terraform-state, AWS cloud, package-registry, SBOM, attestation, provider security-alert, PagerDuty incident-context, and Jira work-item fact intake. | optional `Deployment` | [Collector runtimes](service-runtimes-collectors.md) |
| Scanner Worker | Claim-driven CPU-heavy and memory-heavy security analyzers that emit source facts only. | optional `Deployment` | [Security Intelligence](../reference/security-intelligence.md#scanner-worker-boundary) |
| Bootstrap Index | One-shot initial indexing. | operator-run helper, not chart steady state | [Bootstrap runtimes](service-runtimes-bootstrap.md) |

The direct service binaries are the release artifacts and support `--version`
checks. Helm starts API and MCP through the `eshu` CLI wrapper; most other
workloads use direct `/usr/local/bin/eshu-*` binaries.

Before the schema Job runs, the release binary's embedded Postgres migration
inventory can be inspected with
`eshu-bootstrap-data-plane --postgres-migration-inventory`. This offline JSON
mode needs no database or graph connection and applies no DDL. It reports
`format_version: 1`, `default_variant: "full"`, and rows ordered by migration
path, with `full` before `deferred` for the same path. Each row has `path`,
`variant`, and `checksum_sha256`; deferred rows also have
`full_checksum_sha256`. Compare these with the target database's
`eshu_schema_migrations` receipts. The inventory is derived from embedded SQL
and reports checksums, not applied database state.

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
- `/admin/retire-legacy-workloads` only on the resolution engine (reducer); see
  the [leftover retirement runbook](../operate/legacy-workload-retirement-runbook.md)

Pod readiness gates on the migration ledger. Every Eshu runtime workload
with a `readinessProbe` probes `/readyz`, which includes the `status_schema` check
that the `eshu_schema_migrations` ledger matches the embedded migrations.
That covers the API and MCP Server Deployments as well as the writer
workloads: the ingester `StatefulSet` and the resolution-engine `Deployment`,
including every per-lane resolution-engine `Deployment` (#8242). A rollout
with a pending migration therefore holds both writers not-Ready until the
schema Job finishes, instead of running them against an older schema.

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

**Headerless enforcement for scoped-token-only and OIDC-only deployments
(companion hardening #5394, shipped).** The per-request dev-mode-open branch is
now gated on the same wiring-time predicate as the startup gate — a configured
shared `ESHU_API_KEY`, `ESHU_SCOPED_TOKENS_FILE`, or `ESHU_AUTH_RESOURCE_URI` —
instead of the shared key alone. A scoped-token-only or OIDC-only deployment
therefore starts *and* denies a headerless request to a non-public route with
`401`, the same as a shared-key deployment. History: before the companion,
such a deployment started but still served headerless requests open, so the
interim guidance was to run it behind a network boundary or also set
`ESHU_API_KEY`. That workaround is no longer needed.

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
  Failed, deadline-exceeded, or ambiguous cycles retain the shard lease until
  expiry. Process shutdown that interrupts a cycle before its acceptance-unit
  transaction opened releases the lease immediately; shutdown after that
  point keeps the quarantine, because the commit outcome is unknown.
  Independent shards continue to run.
- `ESHU_REPO_DEPENDENCY_RETRACT_STATEMENT_TIMING` is retained for
  compatibility but no longer changes behavior: repo-dependency retract
  statements always run sequentially with per-statement timing logs (grouped
  DELETEs under-apply on NornicDB v1.1.11).
- Keep claim-driven collectors behind an active workflow coordinator.
- One workflow coordinator replica is the tested shape, and a second one does
  not buy throughput — the collectors do the collecting. A second replica does
  not duplicate work: scheduled-work admission takes a Postgres advisory lock
  per collector instance and compares planned targets on a tuple that ignores
  the schedule bucket, so two coordinators cannot queue the same target twice.
  That was measured for #4586 on the OCI registry scheduler; the other
  schedulers share the same admission path. Two coordinators running at
  different points in the cycle can start the next collection sooner than the
  configured interval once the previous run has finished.
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
- The #7584 activation obligation consumer is **on by default**
  (`ESHU_ACTIVATION_OBLIGATION_CONSUMER_ENABLED=true` since the #7638 flip).
  It settles the obligations `ProjectorQueue.Ack` writes by running the partition-scoped
  deferred maintenance pass on the owed generation only; set the flag `false`
  to restore the old pending-until-retention behavior; see
  [Resolution Engine](../services/resolution-engine.md) "Activation obligations".
  Tune it with `ESHU_ACTIVATION_OBLIGATION_LEASE` (default `30m`,
  arbiter-ruled #7638 item-2 interim: the measured 395 s full-corpus
  984-scope pass fits inside the lease-minus-a-fifth deadline, while the
  2m default timed out),
  `ESHU_ACTIVATION_OBLIGATION_POLL_INTERVAL` (default `10s`), and
  `ESHU_ACTIVATION_OBLIGATION_WORKERS` (default `1`); an unparsable or
  non-positive value falls back to its default without failing startup. Lower
  the lease back only when a rewrite re-measures the pass p99 far below the
  deadline.
- The reducer's graph-backed gauges (`eshu_dp_edges_by_source_tool`,
  `eshu_dp_files_by_language`, `eshu_dp_graph_orphan_nodes`) are served from a
  background snapshot, so a slow graph read can never stall `/metrics` (#7062).
  `ESHU_GRAPH_GAUGE_REFRESH_INTERVAL` (default `5m`) sets how often the
  snapshot is rebuilt and `ESHU_GRAPH_GAUGE_REFRESH_TIMEOUT` (default `30s`)
  bounds each graph read. While refreshes succeed the gauges lag the graph by up
  to one interval plus one read; if they keep failing, the gauges stop
  reporting once the snapshot is three intervals old. Watch `eshu_dp_gauge_snapshot_age_seconds` and
  `eshu_dp_gauge_snapshot_refreshes_total{outcome}` for staleness.
- The reducer rebuilds the hardcoded-secret side table (`content_file_secret_lines`)
  whenever its state is not `ready` (#8833): after a migration that changes the
  derivation function, or a failed or abandoned bootstrap-index finalize. It
  polls the state every `ESHU_SECRET_LINES_REPAIR_POLL_INTERVAL` (default `30s`),
  takes the two bulk-load advisory locks without waiting, and skips while a
  bootstrap-index connection is alive. Its lock session comes from a dedicated
  one-connection pool, so while a secret-lines rebuild runs the reducer opens up
  to one more connection than `ESHU_POSTGRES_MAX_OPEN_CONNS` (see
  [Postgres tuning](../reference/postgres-tuning.md)). It is on by default;
  `ESHU_SECRET_LINES_REPAIR_ENABLED=false` is for diagnostics only. Until the
  state is `ready`, `investigate_hardcoded_secrets` reads use the legacy content
  scan. Every replica runs the loop and the locks let one rebuild at a time.
- The reducer owns the status summary writer (#7009), off by default
  (`ESHU_STATUS_SUMMARY_WRITER_ENABLED=false`). When enabled, every reducer
  replica runs the loop, but a transaction advisory lock lets exactly one
  replica compute per tick (`ESHU_STATUS_SUMMARY_WRITER_INTERVAL`, default `10s`,
  minimum `5s`) and the others count `skipped_lock`. A pass runs the status
  active-work statement, the two Terraform-state admin-evidence statements, and
  the workflow coordinator read (#8828: collector instances, work-item, run and
  completeness counts, claims, backpressure, terminal failures) on the primary and
  stores the results as three rows in `status_summary_snapshots`, each row in its
  own transaction under the same advisory lock, the active-work row first and
  the coordinator row last. A failed or slow companion row never discards an
  earlier row. Watch
  `eshu_dp_status_summary_writer_passes_total{model_key,outcome}` and
  `eshu_dp_status_summary_writer_overrun_total`. The API, MCP server, and
  ingester do not run the writer.
- Every runtime that builds a status store reads the stored summary only when
  `ESHU_STATUS_SUMMARY_READ_ENABLED=true` (default `false`); the API and MCP
  server read it inside the status snapshot transaction. The settings are
  resolved once at process start, and an invalid
  `ESHU_STATUS_SUMMARY_STALE_AFTER` stops the process from starting. A row older than `ESHU_STATUS_SUMMARY_STALE_AFTER`
  (default `33s`), a missing row or table, or a row from another statement
  version runs the live statement for the whole answer; the status payload's
  `active_work_source` object (`terraform_state_source` on the routes that
  render the Terraform-state section and `coordinator_source` on the routes that
  render the coordinator section) and
  `eshu_dp_status_summary_read_total{model_key,source,reason}` report which path
  answered. A runtime's `/metrics` scrape never falls back
  to the live statement: a stale or missing row serves the newest row that
  process can decode (a stale one included, with its true age), or an empty
  summary when it can decode none, and exports
  `eshu_runtime_status_summary_stale 1` and
  `eshu_runtime_status_summary_age_seconds`. The scrape also skips the
  Terraform-state, collector fact-evidence, and registry collector reads it
  never renders, with the reader on or off. See
  [Active Work Source](../reference/http-api/active-work-source.md).

## Route Map

| Need | Use |
| --- | --- |
| Commands, templates, and scaling notes for core services | [Core Runtime Services](service-runtimes-core.md) |
| Collector control-plane and hosted collector matrix | [Collector Runtime Services](service-runtimes-collectors.md) |
| Schema bootstrap and bootstrap-index contract | [Bootstrap Runtime Services](service-runtimes-bootstrap.md) |
| Helm values and render-time guards | [Helm Values](https://github.com/eshu-hq/eshu-deploy/blob/main/docs/kubernetes/helm-values.md) |
| Metrics, traces, logs, and status signal names | [Telemetry Overview](../reference/telemetry/index.md) |
