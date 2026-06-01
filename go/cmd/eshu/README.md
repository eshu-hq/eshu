# eshu

## Purpose

`eshu` is the unified Eshu CLI and service launcher. The same binary drives
local indexing workflows, launches the API and MCP runtimes, owns the
embedded local graph lifecycle, manages graph backend installs, runs
operator/admin workflows, and hosts the `doctor` diagnostic.

## Ownership boundary

This binary owns the Cobra command tree, flag parsing, and local Eshu service
orchestration. It does not own service runtime internals:
`eshu api start` and `eshu mcp start` exec `eshu-api` and `eshu-mcp-server`.
`eshu graph start` owns the local-authoritative supervisor and discovers
`eshu-reducer` and `eshu-ingester` via `PATH`.

## Entry points

- `main` in `go/cmd/eshu/main.go` (delegates to `rootCmd.Execute`)
- root command in `go/cmd/eshu/root.go`
- subcommand groups:
  - service launch: `mcp`, `api`, `serve` plus aliases (`service.go`);
    `version`, `help`, `doctor` (`root.go`, `doctor.go`)
  - indexing: `scan`, `index`, `list`, `stats`, `delete`, `clean`, `query`,
    `watch`, `unwatch`, `watching`, `add-package`, `finalize` plus
    `i`/`ls`/`rm`/`w` aliases (`scan.go`, `basic.go`)
  - security intelligence: `vuln-scan repo [path]` runs the local scan
    readiness contract and reads repository-scoped supply-chain impact findings
    through the API envelope; `vuln-scan provider-parity` compares
    operator-local provider alert summaries to Eshu findings with
    aggregate-only output (`vuln_scan.go`, `vuln_scan_provider_parity.go`)
  - service tracing: `trace service <name>` renders the API service-story
    dossier through a canonical envelope-aware CLI consumer (`trace.go`)
  - documentation truth: `docs verify [path]` verifies local Markdown-family
    documentation claims against the CLI command tree, generated OpenAPI paths,
    and documented Eshu environment variables (`docs.go`)
  - `graph`, `install` with `nornicdb`, `status`, `start`, `stop`,
    `logs`, `upgrade` (`graph.go`, `graph_install.go`,
    `local_graph.go`)
  - `admin`: `facts`, `reindex`, `tuning-report`, `list`, `decisions`,
    `replay`, `dead-letter`, `skip`, `backfill`, `replay-events`
  - `config`, `neo4j`, `find`, `analyze`, `ecosystem`, `workspace`,
    `local-host`

## Configuration

Persistent flags in `root.go`: `--database` sets `ESHU_RUNTIME_DB_TYPE`
for the process; `-V`, `--visual` toggles interactive graph visualization.
Root flags `--version` and `-v`, plus the `eshu version` command, print the
build-time application version from `internal/buildinfo`. Subcommands define
their own flags. Service launch reads the runtime env contract (`ESHU_API_ADDR`,
`ESHU_MCP_TRANSPORT`,
`ESHU_MCP_ADDR`, `ESHU_POSTGRES_DSN`, `ESHU_GRAPH_BACKEND`, `NEO4J_*`).

## Telemetry

The Cobra dispatcher does no OTEL bootstrap. Telemetry runs inside each
launched runtime via the shared `telemetry` package. Errors print to
`os.Stderr`; the binary exits 1 on any Cobra error.

No-Observability-Change: provider-parity lifecycle normalization stays inside
the local CLI and aggregate proof mapping. The Eshu finding read still uses the
existing supply-chain impact API request path, API telemetry, and aggregate
JSON/error output.

No-Regression Evidence: provider-parity lifecycle behavior is covered by
`go test ./cmd/eshu -count=1`.

## Gotchas / invariants

- `SilenceUsage` and `SilenceErrors` are set on the root command
- `eshu graph start` requires `eshu-reducer` and `eshu-ingester` on `PATH`;
  fresh local Eshu service runs need `go/bin` on `PATH` after rebuilding
- `eshu scan` is the readiness contract for one local source. It preflights the
  configured API status surface, launches `eshu-bootstrap-index` with
  `ESHU_REPO_SOURCE_MODE=filesystem`, `ESHU_FILESYSTEM_ROOT` set to the
  resolved source root, `ESHU_FILESYSTEM_DIRECT=true`, and `ESHU_REPOS_DIR`
  under the workspace cache, then polls `/api/v0/status/pipeline` until health
  is `healthy`, queue work is drained, no failures or dead letters exist, and at
  least one generation completed. It also probes `/api/v0/repositories?limit=1`
  before and after the run so the API query surface has to respond, not just the
  status store. It reports bootstrap and queue-zero timings.
  Collector-complete and source-local projection-complete timings remain
  explicit `null` values in JSON because the bootstrap child logs those events
  today but does not expose parent-process structured timestamps.
- `eshu vuln-scan repo [path]` reuses `eshu scan` root resolution, bootstrap,
  and readiness proof before reading
  `/api/v0/supply-chain/impact/findings?repository_id=<id>&limit=<n>`.
  If a service URL is configured by flag, persisted config, or environment, the
  command uses that API. Without a configured service URL, it starts or attaches
  to the workspace-local authoritative owner, launches a short-lived loopback
  `eshu-api` process with that owner's Postgres and graph env, and passes the
  same owner env to `eshu-bootstrap-index` so writes and reads use the same
  local stores.
  `--repo-id` bypasses repository selector resolution when the caller already
  knows the exact repository id. The command exits fail-closed when the scan is
  submitted, partial, failed, or cannot resolve a repository; it does not query
  findings or print a clean zero-finding state until the target is ready.
  The command is an API-backed reader and must not open graph or Postgres
  connections directly.
  The default scope mode is `scoped`: the CLI derives observed-dependency
  facts, advisory facts, package-registry facts and freshness, source-snapshot
  diagnostics, and the envelope-aggregate freshness from the readiness
  envelope. The package metadata guard fires whenever a `ready_*` response is
  backed by missing or non-fresh `package.registry` evidence; the CLI
  downgrades to `evidence_incomplete` and records
  `package_registry_metadata`. The scoped advisory guard also fires when the
  envelope's aggregate `freshness` is `stale` and the server still returned a
  `ready_*` state; in that case the CLI records `advisory_cache_stale`.
  Per-source `source_snapshots[]` entries are surfaced for visibility while
  the CLI gates on the server-owned aggregate scoped freshness verdict.
  `--broad` skips the advisory scoped guard, records a warning that the
  wider mode bypassed it, and surfaces `data.scope_mode = "broad"` so
  operators can tell the modes apart in JSON output; it still fails closed on
  stale or missing package-registry metadata. The `*_facts` fields are counts
  of source facts (the same `evidence_sources[].fact_count` the server
  reports); `package_registry_facts` counts metadata only for the requested
  package or for packages already tied to the requested repository by
  consumption evidence. Every run attaches a
  `data.scan_performance` block with started_at, completed_at, wall_time_ms,
  repository_size_bytes, repository_file_count, observed_dependency_facts,
  advisory_facts, package_registry_facts, package_registry_freshness,
  package_registry_complete, cache_freshness, scope_mode, and stop_threshold
  so the local one-shot scan ships its own performance evidence without a
  separate measurement step. JSON output also includes
  `data.report.schema_version = "eshu.vulnerability_report.v1"` with the
  scanner summary, readiness, freshness, unsupported targets, target/package
  context, evidence handles, remediation metadata, scope plan, and performance
  block. Scoped mode treats stale or unknown aggregate freshness as
  `evidence_incomplete`. The command exits `0` for ready-zero, `3` for
  findings, `4` for non-ready evidence, `5` for unsupported target evidence,
  and `1` for runtime or transport failures before readiness is classified.
  `--export sarif` writes SARIF v2.1.0 to stdout from the same scanner report:
  reducer-owned findings become SARIF results, source paths become locations
  only when the API provided them, and run properties preserve readiness,
  missing-evidence, unsupported-target, scope-mode, and exit-code context.
  `--json` and `--export sarif` are mutually exclusive output contracts.
- `eshu vuln-scan provider-parity` is the private-safe provider alert proof
  wrapper. It reads an operator-local allowlist file, optionally reads a local
  generic provider summary file, or fetches GitHub Dependabot alert summaries
  using a token from the named environment variable. It calls only the bounded
  Eshu supply-chain impact API and returns aggregate class counts. The command
  must not print repository names, repository ids, package names, package ids,
  advisory ids, CVE ids, alert URLs, tokens, provider payloads, or Eshu finding
  rows. Provider lifecycle state is evidence, not active-impact truth:
  fixed/closed and dismissed/suppressed provider rows do not become reducer bug
  candidates unless Eshu has a conflicting row, and stale readiness evidence is
  treated as missing evidence for parity.
- `eshu trace service <name>` is a read-only CLI consumer of
  `/api/v0/services/{service_name}/story`. It asks the API for
  `application/eshu.envelope+json`, passes supported selectors through as
  `repo`, `environment`, and `service_id` query parameters, renders the service
  identity, repository, materialization status, code-to-runtime evidence
  segments, deployment-lane count, runtime-instance count, upstream/downstream
  counts, coverage, and limitations, and preserves the full canonical envelope
  with `--json`.
  Ambiguous names print the candidate service ids and exit `3`; stale or
  building truth freshness exits `4`; partial code-to-runtime traces exit `5`
  while still printing the usable evidence. The CLI must not open graph or
  Postgres connections directly for this path.
- `eshu docs verify [path]` is a local documentation-truth verifier. It scans
  Markdown-family files with `--limit` and `--max-bytes`, extracts explicit
  Eshu CLI command claims, HTTP endpoint claims, `ESHU_*` environment-variable
  claims, explicit local repo path claims, tagged or digested container image
  refs, Terraform block addresses, and known unsupported shell-command claims,
  then generates documentation finding and evidence-packet fact envelopes in
  memory. Local path and Terraform address claims are checked against the nearest
  Git worktree root or current working directory. Missing files are contradicted
  findings, and missing Terraform blocks are contradicted only when the local
  Terraform truth scan completes cleanly; invalid, oversized, or incomplete
  Terraform truth is reported as missing evidence. Without `--persist`, it does
  not open Postgres or graph connections. With
  `--persist`, it opens the shared Postgres fact-store DSN, writes a
  documentation-source scope generation, and skips re-verification when the
  current pending or active generation has the same document fingerprint while
  still returning persisted findings for `--fail-on` evaluation.
- `eshu mcp start --workspace-root <repo>` attaches to the active local owner.
  The stdio path execs the internal `local-host mcp-stdio` attach command, while
  `--transport http` and legacy `--transport sse` exec `eshu-mcp-server` with
  the owner-derived Postgres DSN, graph backend, graph URI, and workspace
  credentials. HTTP attach fails fast if the owner record, Postgres socket, or
  graph backend health probe is not ready.
- `eshu graph start` acquires `owner.lock` through the local host startup path
  before embedded Postgres starts. If an earlier shutdown removed `owner.json`
  but left a live workspace `postmaster.pid`, startup verifies PID liveness,
  socket health, and the Postgres protocol before running `pg_ctl stop` and
  starting a fresh embedded Postgres.
- `local_authoritative` rebuilds from the workspace source tree on owner start,
  so startup clears the rebuildable Postgres `data` / `runtime` directories and
  the local NornicDB graph store before launching children. It also clears the
  filesystem selector manifest under `cache/repos` so a restarted owner cannot
  mistake an empty fresh Postgres for an unchanged source tree. The reset
  preserves managed Postgres binaries and logs while avoiding stale queue rows,
  old graph nodes, and NornicDB search-index warmup over obsolete data
  (`local_host_reset.go`).
- For `local_authoritative` + NornicDB, the local owner sets snapshot, parse,
  projector, and reducer worker env vars to the developer machine's CPU count
  before launching `eshu-ingester` and `eshu-reducer`. Explicit env vars still
  win, so a developer can lower or raise a single pool without changing the
  owner code (`local_host_config.go` and `local_host.go`).
- Foreground `eshu graph start` defaults child service logs to workspace log
  files (`eshu-ingester.log`, `eshu-reducer.log`) while `--progress auto`
  renders a branded Bubble Tea progress panel on the terminal alternate screen.
  The panel leads with a verdict (`Watching`, `Indexing`, `Settling`,
  `Complete`, or `Attention`) and uses animated Ember-to-Signal-Teal bars with
  stage states and known-work denominators: collector generations and
  projector/reducer work items. An active collector generation is the current
  snapshot and counts as done in this table; pending collector generations keep
  the verdict at `Indexing` until the collector settles. `Complete` means every
  known stage has drained; if shared projection intents still need to become
  graph-visible, the verdict stays at `Settling` and the panel prints a
  `Shared projections` backlog line with outstanding and in-flight counts. The
  table pads columns by display width, so colored progress bars do not shift
  the `Done`, `Active`, `Waiting`, or `Failed` counts. It shows `idle` when the
  status store has no active denominator yet. `--progress plain` writes
  append-only text snapshots, `--verbose` and `--logs terminal` restore direct
  terminal logs for debugging, `--logs quiet` discards child logs, and
  `--progress quiet` suppresses the progress reporter.
- `graphBoltHealthy` sends the Bolt magic + four version proposals and reads
  the 4-byte server response. The response must match one offered protocol
  version; `00 00 00 00` means the server rejected negotiation and is not ready.
  A TCP-only dial is insufficient because embedded NornicDB accepts connections
  before the Bolt protocol handler is fully ready, causing a handshake EOF on
  the first schema bootstrap attempt.
- `eshu graph stop` sends `SIGTERM` to the owner supervisor for both
  `local_lightweight` and `local_authoritative` profiles only after ownership
  checks pass. Lightweight stop requires the recorded Postgres socket to be
  healthy before signaling the owner PID; otherwise it acquires `owner.lock`,
  stops any recorded embedded Postgres child, and only then removes stale
  metadata. Authoritative stop uses the same lock-before-reclaim discipline when
  the owner PID is already gone: if the graph is unhealthy, it stops any
  recorded embedded Postgres child and removes stale metadata. If the lock is
  still held, the record is preserved for the running owner or the next reclaim
  path. Authoritative stop additionally waits for the graph sidecar (NornicDB)
  to become unreachable.
- The default local graph path is embedded NornicDB when `eshu` is built with
  `nolocalllm`; `ESHU_NORNICDB_RUNTIME=process` is the only runtime-mode
  override, while `ESHU_NORNICDB_BINARY` only chooses the specific backend
  binary after process mode is selected
- Embedded NornicDB writes its effective runtime settings to
  `graph-nornicdb.log` after `nornicdb.Open` applies library defaults. The line
  includes parallel execution, worker count, memory limit, GC percent, object
  pooling, query cache, embedding, Heimdall, and Qdrant gRPC state so
  performance runs can cite the actual active settings rather than inferred
  defaults.
- Embedded and process NornicDB both use the per-workspace credentials written
  under the local graph data directory; child services receive the same values
  through `ESHU_NEO4J_USERNAME`, `ESHU_NEO4J_PASSWORD`, `NEO4J_USERNAME`, and
  `NEO4J_PASSWORD`
- Embedded NornicDB must wire Bolt through the HTTP server's role, database
  access, and resolved-access callbacks. Without that shared RBAC path,
  authenticated child services can connect but projector writes to the default
  `nornic` database fail with a Neo4j security-forbidden error.
- `--database` mutates the process environment via `os.Setenv`

## Related docs

- [Service runtimes](../../../docs/public/deployment/service-runtimes.md)
- [CLI reference](../../../docs/public/reference/cli-reference.md)
- [CLI indexing](../../../docs/public/reference/cli-indexing.md)
