# CLI Reference

This is the single-page reference for the public `eshu` CLI.

Use this page when you need command lookup material: command names, flags,
remote/API behavior, and config keys.

For task workflows, start with:

- [Index repositories](../use/index-repositories.md)
- [Ask code questions](../use/code-questions.md)
- [Trace infrastructure](../use/trace-infrastructure.md)
- [Connect MCP](../mcp/index.md)

## How the CLI works

`eshu` has two public command families:

- Local Eshu service commands start or manage local processes and files.
  `eshu index` launches `eshu-bootstrap-index` and writes to the configured
  Postgres and graph stores.
- API-backed commands call a Eshu HTTP API. `eshu list`, `eshu stats`,
  `eshu index-status`, `eshu find ...`, and `eshu analyze ...` are API clients.

The API client resolves its base URL from command flags, config, environment,
then the built-in default `http://localhost:8080`. Local Docker Compose
publishes the API on that default port.

Remote/API facts for this release:

- CLI read commands use the HTTP API, not MCP.
- Commands with `--service-url`, `--api-key`, and `--profile` can be pointed at
  a deployed API directly.
- `eshu list` and `eshu stats` are API-backed but currently resolve the API URL
  from config or environment rather than per-command flags.
- Remote `find` and `analyze` commands do not support `--visual`.
- `eshu admin reindex` queues a reindex request for the ingester to execute. The API process does not do the full reindex work inline.
- `eshu admin facts replay` replays dead-lettered facts-first work items back
  to `pending`. It also accepts `failed` rows when older terminal entries are
  still present. It requires at least one selector so operators do not
  accidentally replay the entire terminal set.
- `eshu admin facts dead-letter` moves selected work items into durable terminal state with an operator note.
- `eshu admin facts backfill` creates a durable backfill request for a repository or source run.
- `eshu admin facts replay-events` lists durable replay audit rows for incident review.

Hidden internal runtime commands exist for service containers, but this page documents the public CLI surface.

## Global options

These options apply at the root command level.

| Option | What it does |
| :--- | :--- |
| `--database`, `-db` | Temporarily switch the database backend for one command. |
| `--visual`, `--viz`, `-V` | Ask supported local `find` and `analyze` commands to open graph-style visualization output. |
| `--workspace-root` | Pin the workspace-root directory explicitly. Overrides the resolution order described in [Workspace root and profiles](#workspace-root-and-profiles). Applies to `eshu watch` and `eshu workspace watch`. |
| `--version`, `-v` | Show the installed Eshu version and exit. |
| `--help`, `-h` | Show help and exit. |

For `eshu`, release and local installer builds report the version injected at
build time. A plain `go install ...@vX.Y.Z` binary reports Go's embedded module
version. Local source builds without a version override report `dev`.

## Service binary version checks

The installed service binaries also accept `--version` and `-v` as a single
argument. They print their build-time version and exit before telemetry,
Postgres, graph, queue, or HTTP startup:

```bash
eshu-api --version
eshu-mcp-server -v
eshu-ingester --version
eshu-reducer --version
eshu-bootstrap-index --version
eshu-bootstrap-data-plane --version
eshu-workflow-coordinator --version
eshu-projector --version
eshu-collector-git --version
eshu-admin-status --version
```

### Runtime profile

The CLI, MCP server, and HTTP API all accept the same runtime-profile axis via
the `ESHU_QUERY_PROFILE` environment variable. Allowed values:
`local_lightweight`, `local_authoritative`, `local_full_stack`, `production`.
Invalid values are rejected at startup. Local-host entrypoints choose their
profile explicitly from command context, while hosted API and MCP runtimes
default to `production` when `ESHU_QUERY_PROFILE` is unset.
Truth-level behavior per profile is defined by
[Capability Conformance Spec](capability-conformance-spec.md) and
[Truth Label Protocol](truth-label-protocol.md).

### Graph backend

Separately from profile, Eshu selects a graph adapter via
`ESHU_GRAPH_BACKEND`. Allowed values: `nornicdb` (default), `neo4j`.
Invalid values are rejected at startup. See
[Graph Backend Installation](graph-backend-installation.md) and
[ADR 2026-04-22](../adrs/2026-04-22-nornicdb-graph-backend-candidate.md)
for the backend history and compatibility notes.

`local_authoritative` auto-manages NornicDB. A `eshu` binary built with
`-tags nolocalllm` runs NornicDB in-process, which is the normal local binary
path. Users do not need to install `nornicdb-headless` for that mode.

External process mode remains available when maintainers need to test a
specific NornicDB build. Set `ESHU_NORNICDB_RUNTIME=process`, or set
`ESHU_NORNICDB_BINARY` directly. Process-mode binary discovery uses this order:

1. `ESHU_NORNICDB_BINARY`
2. `${ESHU_HOME}/bin/nornicdb-headless` installed by
   `eshu install nornicdb --from <source>`
3. `nornicdb-headless` in `PATH`
4. `nornicdb` in `PATH`

With the NornicDB default backend, local-authoritative canonical graph writes
use bounded phase-group transactions and are still bounded by
`ESHU_CANONICAL_WRITE_TIMEOUT` (`30s` by default). The default phase-group size
is `500` statements and can be tuned with
`ESHU_NORNICDB_PHASE_GROUP_STATEMENTS=<positive integer>` when repo-scale
dogfood runs need a larger or smaller transaction window. This protects local
MCP/CLI coding workflows from an indefinitely stuck graph write while keeping
content-index-backed code search available even when graph projection is
degraded.
For the full NornicDB tuning matrix, including file/entity row caps and
conformance-only switches, see [NornicDB Tuning](nornicdb-tuning.md).

`ESHU_NORNICDB_CANONICAL_GROUPED_WRITES=true` is reserved for NornicDB adapter
conformance runs. It enables the same grouped canonical write surface used by
Neo4j so Eshu can prove rollback, timeout, and no-partial-write behavior.
Keep the switch unset for normal laptop coding. Manual conformance runs must
use a disposable `ESHU_HOME` / workspace data root.

### Graph backend commands

The `local_authoritative` profile runs NornicDB inside the local `eshu` process
managed by the local Eshu service. Eshu exposes:

| Command | Purpose |
| :--- | :--- |
| `eshu graph status` | Available now. Report local Eshu service metadata, backend, PID, binary path, ports, log path, and current running state when present. |
| `eshu install nornicdb [--from <source>] [--sha256 <hex>] [--force] [--full]` | Available now for process-mode testing. Normal embedded local runs do not require it. Eshu currently tracks the latest NornicDB `main` branch, so the working process install path is an explicit binary, tar archive, macOS package, or URL that you built or chose from that branch. The command verifies the binary, copies it to `${ESHU_HOME}/bin/nornicdb-headless`, and records the source and checksums in the managed install manifest. Bare no-argument installs are accepted by the CLI but fail until an accepted manifest policy exists; `--full` is reserved for that future no-argument release flow. Remote downloads honor `Ctrl-C` and default to `30s`; override with `ESHU_NORNICDB_INSTALL_TIMEOUT=<duration>` when slower links need more time. Signature verification remains future work. |
| `eshu graph logs [--workspace-root <path>]` | Available now. Print the current workspace `graph-nornicdb.log` file if present. Child service logs for foreground `graph start` runs are written under the same workspace log directory as `eshu-ingester.log` and `eshu-reducer.log` unless terminal logging is requested. |
| `eshu graph stop [--workspace-root <path>]` | Available now. Request the local Eshu service to shut down so embedded NornicDB, embedded Postgres, and child runtimes stop through the normal lifecycle; stale process-mode graph processes are stopped directly. |
| `eshu graph start [--workspace-root <path>] [--progress auto\|plain\|quiet] [--logs file\|terminal\|quiet] [--verbose]` | Available now. Foreground shortcut for starting the `local_authoritative` local Eshu service, equivalent to `ESHU_QUERY_PROFILE=local_authoritative eshu watch .`. With a `-tags nolocalllm` build, this starts embedded Postgres and embedded NornicDB in the `eshu` process, then starts the ingester and reducer. By default, foreground mode keeps structured child logs in workspace log files and renders a branded animated Bubble Tea progress panel on the terminal alternate screen when stderr is a terminal. The panel leads with `Watching`, `Indexing`, `Settling`, `Complete`, or `Attention`, then shows known-work denominators for collector generations plus projector/reducer work items; superseded generations are shown as context and excluded from the active collector denominator, and empty status rows render as `idle` instead of `0/0`. Use `--progress plain` for append-only text snapshots, `--progress quiet` for script-friendly foreground runs, and `--verbose` or `--logs terminal` when debugging child JSON logs directly. |
| `eshu graph upgrade --from <source> [--sha256 <hex>] [--workspace-root <path>]` | Available now for process-mode testing. Replace the managed NornicDB binary from a verified local binary, tar archive, macOS package, or URL; requires the workspace graph to be stopped first. |

Full operator contract: [Graph Backend Operations](graph-backend-operations.md).

## Workspace root and profiles

The local Eshu service treats each workspace as a single-service filesystem.
A workspace has one data root at `${ESHU_HOME}/local/workspaces/<workspace_id>/`.

### Resolution order

When you run `eshu watch .`, `eshu mcp stdio`, or any command that needs a
workspace, Eshu picks the workspace root in this order:

1. `--workspace-root <path>` explicit flag
2. Nearest ancestor directory containing `.eshu.yaml`
3. Nearest ancestor directory containing `.git`
4. The current working directory

The resolved path is passed through `realpath`, normalized, and hashed
(SHA-256, first 20 bytes hex) to derive a stable `workspace_id`. Two symlinked
paths that resolve to the same real path converge to the same `workspace_id`.

### ESHU_HOME defaults

`ESHU_HOME` controls where local Eshu service state lives. Override with the
`ESHU_HOME` environment variable. Defaults:

| OS | Default |
| --- | --- |
| macOS | `~/Library/Application Support/eshu` |
| Linux | `${XDG_DATA_HOME:-~/.local/share}/eshu` |
| Windows | `%LOCALAPPDATA%\eshu` (ownership + transport deferred) |

### Data-root layout

Each workspace owns one directory tree under `${ESHU_HOME}/local/workspaces/<workspace_id>/`:

```text
VERSION            # layout schema version
owner.lock         # flock sentinel for the single-service invariant
owner.json         # current service metadata (PID, postgres state, optional graph state)
graph/             # optional authoritative graph backend data root
postgres/          # embedded Postgres data directory
logs/              # local-host lifecycle and recovery logs
cache/             # derived local caches (rebuildable)
```

See [Local Data Root Spec](local-data-root-spec.md) and
[Local Eshu Service Lifecycle](local-host-lifecycle.md) for the full contract.

## Public command map

### Root commands

| Command | Purpose | API-backed |
| :--- | :--- | :--- |
| `eshu help` | Show the full root help screen. | No |
| `eshu version` | Print the installed version. | No |
| `eshu doctor` | Run local diagnostics. | No |
| `eshu index [path] [--discovery-report <file>]` | Index a local path by launching the Go `bootstrap-index` runtime. The optional discovery report writes a JSON advisory artifact for noisy-repo tuning. | No |
| `eshu index-status` | Show the latest checkpointed index status. This is the completeness signal, not process health. | Yes |
| `eshu finalize` | Compatibility stub. Prints the current ingester recovery endpoints and exits non-zero. | No |
| `eshu clean` | Compatibility stub. Prints cleanup guidance and exits non-zero. | No |
| `eshu stats [repo-or-path]` | Show indexing statistics. Existing local paths are normalized to absolute indexed paths; other arguments are treated as repository selectors such as name or repo slug. | Yes |
| `eshu delete <path>` | Compatibility stub. Prints deletion guidance and exits non-zero. | No |
| `eshu delete --all` | Compatibility stub. Prints deletion guidance and exits non-zero. | No |
| `eshu list` | List indexed repositories. | Yes |
| `eshu add-package` | Compatibility stub. Prints package-indexing guidance and exits non-zero. | No |
| `eshu watch [path]` | Watch a local path and keep the graph updated. In local-host mode it now prints a live progress panel for indexing and projection instead of a fake percentage bar. | No |
| `eshu unwatch <path>` | Compatibility stub. Prints watcher-lifecycle guidance and exits non-zero. | No |
| `eshu watching` | Compatibility stub. Prints watcher-lifecycle guidance and exits non-zero. | No |
| `eshu query "<query>"` | Run a language-query search against indexed code. | No |
| `eshu start` | Deprecated root alias for `eshu mcp start`. | No |

`eshu index --discovery-report <file>` is intentionally file-based instead of
metric-based: it can include repository paths, top noisy directories/files,
entity counts, and skip breakdowns without putting those high-cardinality
values into Prometheus labels. Use it after a timeout-heavy or unexpectedly
large run to decide whether a repo-local `.eshu/discovery.json` map is the right
fix.

### Workspace commands

`eshu workspace` is the shared-workspace command group.

| Command | Purpose | API-backed |
| :--- | :--- | :--- |
| `eshu workspace plan` | Queue a workspace reindex plan through the Go admin reindex flow. | No |
| `eshu workspace sync` | Queue a workspace sync through the Go admin reindex flow. | No |
| `eshu workspace index` | Queue a workspace index through the Go admin reindex flow. | No |
| `eshu workspace status` | Show workspace path and latest index summary. | Yes |
| `eshu workspace watch` | Watch the materialized workspace. | No |

### Search commands

`eshu find` is for lookup and discovery in the graph.

| Command | Purpose | API-backed |
| :--- | :--- | :--- |
| `eshu find name <name>` | Exact-name search. | Yes |
| `eshu find pattern <text>` | Substring search. | Yes |
| `eshu find type <type>` | List all nodes of one type. | Yes |
| `eshu find variable <name>` | Find variables by name. | Yes |
| `eshu find content <text>` | Full-text search in source content. | Yes |
| `eshu find decorator <name>` | Find functions with a decorator. | Yes |
| `eshu find argument <name>` | Find functions with a parameter name. | Yes |

### Analysis commands

`eshu analyze` is for graph relationships and code quality signals.

| Command | Purpose | API-backed |
| :--- | :--- | :--- |
| `eshu analyze calls <function>` | Show what a function calls. Supports `--transitive` and `--depth`. | Yes |
| `eshu analyze callers <function>` | Show what calls a function. Supports `--transitive` and `--depth`. | Yes |
| `eshu analyze chain <from> <to>` | Show the call chain between two functions. Supports `--repo`, `--repo-id`, and `--depth`; repo-scoped names are resolved to entity IDs, and if a name is ambiguous the API uses graph reachability as the tie-breaker when exactly one candidate pair is reachable. | Yes |
| `eshu analyze deps <module>` | Show import and dependency relationships. | Yes |
| `eshu analyze tree <class>` | Show inheritance hierarchy. | Yes |
| `eshu analyze complexity` | Show relationship-based complexity metrics for one entity. | Yes |
| `eshu analyze dead-code` | Find derived dead-code candidates after default entrypoint, direct Go Cobra/stdlib-HTTP/controller-runtime signature, Go public-API, test, and generated-code exclusions, with optional `--repo` (ID, name, slug, or path), `--repo-id`, `--limit`, `--exclude`, and `--fail-on-found`. | Yes |
| `eshu analyze overrides <name>` | Find implementations across classes. | Yes |
| `eshu analyze variable <name>` | Show variable definitions and usage. | Yes |

### Admin commands

| Command | Purpose | API-backed |
| :--- | :--- | :--- |
| `eshu admin reindex` | Queue a remote ingester reindex request. | Yes |
| `eshu admin tuning-report` | Show shared-projection tuning state from the admin API. | Yes |
| `eshu admin facts replay` | Replay failed facts-first work items through the admin API. | Yes |
| `eshu admin facts dead-letter` | Move selected facts-first work items into terminal dead-letter state. | Yes |
| `eshu admin facts skip` | Mark selected facts-first work items as skipped with an operator note. | Yes |
| `eshu admin facts backfill` | Create a durable fact backfill request. | Yes |
| `eshu admin facts list` | List fact work items and durable failure metadata. | Yes |
| `eshu admin facts decisions` | List persisted projection decisions and optional evidence. | Yes |
| `eshu admin facts replay-events` | List durable replay audit rows. | Yes |

### Service, setup, and config commands

| Command | Purpose |
| :--- | :--- |
| `eshu graph status` | Show the current local Eshu service metadata and runtime state. |
| `eshu graph logs [--workspace-root <path>]` | Print the current workspace graph-backend log file if present. |
| `eshu graph stop [--workspace-root <path>]` | Request graph shutdown through the local Eshu service, or stop a stale recorded graph process when the service is already dead. |
| `eshu graph start [--workspace-root <path>] [--progress auto\|plain\|quiet] [--logs file\|terminal\|quiet] [--verbose]` | Start the `local_authoritative` local Eshu service in the foreground. Default child logs go to workspace log files; `--progress auto` uses a branded animated Bubble Tea known-work panel on terminals; `--verbose` sends child logs to the terminal. |
| `eshu graph upgrade --from <source> [--sha256 <hex>] [--workspace-root <path>]` | Replace the managed process-mode graph binary from a binary path, tar archive, macOS package, or URL after the workspace graph is stopped. |
| `eshu install nornicdb [--from <source>] [--sha256 <hex>] [--force] [--full]` | Install a verified latest-main NornicDB binary into the managed Eshu home for explicit process-mode testing. Normal embedded local mode does not require this command. Bare no-argument installs and `--full` are present in the CLI but reserved for a future accepted manifest policy. |
| `eshu mcp setup` | Configure IDE and CLI MCP integrations. |
| `eshu mcp start` | Start the MCP server. |
| `eshu mcp tools` | List MCP tools. |
| `eshu api start` | Start the HTTP API server. |
| `eshu serve start` | Start the HTTP API runtime convenience process. Use `eshu mcp start` for MCP. |
| `eshu neo4j setup` | Configure a Neo4j connection. |
| `eshu config show` | Show current config values. |
| `eshu config set <key> <value>` | Set one config value. |
| `eshu config reset` | Reset config to defaults. |
| `eshu config db <backend>` | Quickly switch the default database backend. |

### Ecosystem commands

`eshu ecosystem` is for cross-repository workflows.

| Command | Purpose |
| :--- | :--- |
| `eshu ecosystem index` | Compatibility stub. Prints guidance toward `eshu index`, `eshu workspace index`, or admin reindex flows. |
| `eshu ecosystem status` | Compatibility stub. Prints guidance toward `eshu index-status`, `eshu workspace status`, or admin/status APIs. |
| `eshu ecosystem overview` | Show ecosystem summary statistics. |

### Shortcuts

These are public aliases:

| Shortcut | Expands to |
| :--- | :--- |
| `eshu m` | `eshu mcp setup` |
| `eshu n` | `eshu neo4j setup` |
| `eshu i` | `eshu index` |
| `eshu ls` | `eshu list` |
| `eshu rm` | `eshu delete` compatibility stub |
| `eshu w` | `eshu watch` |

## API URL and profiles

### Commands with per-command API flags

These commands accept `--service-url`, `--api-key`, and `--profile`:

- `eshu index-status`
- `eshu workspace status`
- `eshu admin reindex`
- `eshu admin tuning-report`
- `eshu admin facts replay`
- `eshu admin facts dead-letter`
- `eshu admin facts skip`
- `eshu admin facts backfill`
- `eshu admin facts list`
- `eshu admin facts decisions`
- `eshu admin facts replay-events`
- `eshu find name`
- `eshu find pattern`
- `eshu find type`
- `eshu find variable`
- `eshu find content`
- `eshu find decorator`
- `eshu find argument`
- `eshu analyze calls`
- `eshu analyze callers`
- `eshu analyze chain`
- `eshu analyze deps`
- `eshu analyze tree`
- `eshu analyze complexity`
- `eshu analyze dead-code`
- `eshu analyze overrides`
- `eshu analyze variable`

`eshu list` and `eshu stats` also call the API, but they currently use config,
environment, or the built-in default URL rather than per-command API flags.

### Per-command API flags

Commands listed above use the same flag pattern:

- `--service-url`: remote HTTP base URL
- `--api-key`: bearer token
- `--profile`: named profile for resolving service URL and token

### API config keys

You can avoid repeating API flags by storing config values.

Shared keys:

- `ESHU_SERVICE_URL`
- `ESHU_API_KEY`
- `ESHU_SERVICE_PROFILE`
- `ESHU_REMOTE_TIMEOUT_SECONDS`

Profile-specific keys:

- `ESHU_SERVICE_URL_<PROFILE>`
- `ESHU_API_KEY_<PROFILE>`

Example:

```bash
eshu config set ESHU_SERVICE_URL_QA https://eshu.qa.example.test
eshu config set ESHU_API_KEY_QA your-token-here
eshu config set ESHU_SERVICE_PROFILE QA
```

Then you can run:

```bash
eshu workspace status --profile qa
eshu find name handle_payment --profile qa
eshu admin reindex --profile qa
eshu admin facts replay --profile qa --work-item-id fact-work-123
eshu admin facts list --profile qa --status failed
eshu admin facts decisions --profile qa --repository-id repository:r_payments --source-run-id run-123
```

### API examples

Check workspace status against a deployed API:

```bash
eshu workspace status --service-url https://eshu.qa.example.test --api-key "$ESHU_API_KEY"
```

Check checkpointed status:

```bash
eshu index-status --profile qa
```

Treat `eshu index-status` as the latest checkpoint-completeness view. Use the
runtime health/admin/status surfaces for liveness and stage progress instead.

Queue a full workspace rebuild on a deployed ingester:

```bash
eshu admin reindex --profile qa --ingester repository --scope workspace --force
```

Replay one dead-lettered facts-first work item:

```bash
eshu admin facts replay --profile qa --work-item-id fact-work-123
```

Replay failed facts-first work for one repository:

```bash
eshu admin facts replay --profile qa --repository-id repository:r_payments --limit 25
```

Run query commands against a deployed API:

```bash
eshu find name handle_payment --profile qa
eshu analyze callers handle_payment --profile qa
eshu analyze complexity --profile qa
```

## Workflow entry points

Keep this page as a reference. Use the task pages for beginner workflows and
examples:

- [Index repositories](../use/index-repositories.md)
- [Ask code questions](../use/code-questions.md)
- [Trace infrastructure](../use/trace-infrastructure.md)
- [Connect MCP](../mcp/index.md)

## Related docs

- [CLI K.I.S.S.](cli-kiss.md)
- [CLI: Indexing & Management](cli-indexing.md)
- [CLI: Analysis & Search](cli-analysis.md)
- [CLI: System](cli-system.md)
- [Configuration](configuration.md)
- [HTTP API](http-api.md)
