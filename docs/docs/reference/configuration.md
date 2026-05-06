# Configuration & Settings

Eshu is highly configurable through environment files and the CLI.

## `eshu config` Command

View and modify settings directly from the terminal.

### 1. View Settings
Shows the current effective configuration (merged from defaults and `.env`).

```bash
eshu config show
```

### 2. Set a Value
Update a setting permanently. This writes to `~/.eshu/.env`.

**Syntax:** `eshu config set <KEY> <VALUE>`

```bash
# Switch to the explicit Neo4j backend
eshu config db neo4j

# Select filesystem discovery for a local workspace
eshu config set ESHU_REPO_SOURCE_MODE filesystem

# Persist a local API bearer token for CLI calls
eshu config set ESHU_API_KEY local-compose-token
```

The user-level `~/.eshu/.env` file is for CLI settings, not
the local API bearer-token contract. When local compose auth is enabled, the
Go API and MCP runtimes use `ESHU_API_KEY` from the running container
environment.

### 3. Quick Switch Database
A shortcut to toggle between the default NornicDB backend and the explicit
Neo4j backend.

```bash
eshu config db nornicdb
eshu config db neo4j
```

---

## Configuration Reference

Here are the common settings you can configure. For the complete operator
catalog, including defaults, owner runtime, and when to tune each knob, use the
[Environment Variables](environment-variables.md) reference.

### Core Settings

| Key | Default | Description |
| :--- | :--- | :--- |
| **`ESHU_GRAPH_BACKEND`** | `nornicdb` | Graph adapter to use (`nornicdb` or `neo4j`). |
| **`DEFAULT_DATABASE`** | `nornic` | Bolt database name used by the selected graph backend. |
| **`ESHU_NEO4J_DATABASE`** | `nornic` | Bolt database name passed through the Neo4j driver compatibility layer. |

### Logging And Tracing

These settings control the shared structured logging and OTEL tracing behavior
used by the API, MCP server, ingester, reducer, bootstrap flows, and local
CLI.

| Key | Default | Description |
| :--- | :--- | :--- |
| **`OTEL_EXPORTER_OTLP_ENDPOINT`** | unset | Enables OTLP trace and metric export for Go runtimes when set. |

Notes:

- Current Go runtimes emit newline-delimited JSON logs on stderr.
- Logs are intentionally shaped for generic collectors. Loki, Elasticsearch, and similar backends can treat each line as one JSON document.
- Every log record uses the same top-level envelope and stores custom dimensions under `extra_keys`.
- Trace correlation is automatic when a log is emitted inside an active OTEL span.
- The request ID becomes the default correlation ID unless an upstream correlation ID is already present.
- OTEL logs export is not required for this setup. JSON stderr logs are the source of truth for logs.

### Concurrency Controls

These settings are the public knobs for the collector, projector,
bootstrap, reducer, and watch flows.

| Key | Default | Description |
| :--- | :--- | :--- |
| **`ESHU_SNAPSHOT_WORKERS`** | `min(NumCPU, 8)` | Concurrent repository snapshot workers for collector/bootstrap discovery and collection. |
| **`ESHU_PARSE_WORKERS`** | `min(NumCPU, 8)` | Concurrent file-parse workers inside a repository snapshot. |
| **`ESHU_STREAM_BUFFER`** | `0` | Optional buffer for streaming collected generations. `0` means use the worker-count-derived default. |
| **`ESHU_LARGE_REPO_FILE_THRESHOLD`** | `1000` | File-count threshold above which a repository is treated as “large” for concurrency limiting. |
| **`ESHU_LARGE_REPO_MAX_CONCURRENT`** | `2` | Maximum number of large repositories that may be snapshotted concurrently. |
| **`ESHU_PROJECTOR_WORKERS`** | Default: `min(NumCPU, 8)`; NornicDB local-authoritative: `1` | Concurrent source-local projection workers in the ingester runtime. |
| **`ESHU_LARGE_GEN_THRESHOLD`** | `10000` | Fact-count threshold above which a projector generation is treated as “large”. |
| **`ESHU_LARGE_GEN_MAX_CONCURRENT`** | Default: `2`; local-authoritative: `4` | Maximum number of large projector generations processed concurrently. |
| **`ESHU_PROJECTION_WORKERS`** | `min(NumCPU, 8)` | Concurrent bootstrap-index projection workers. |
| **`ESHU_REDUCER_WORKERS`** | Neo4j: `min(NumCPU, 4)`; NornicDB: `min(NumCPU, 8)` | Concurrent reducer intent workers in the resolution engine. |
| **`ESHU_REDUCER_BATCH_CLAIM_SIZE`** | Neo4j: `workers * 4` (min 4, max 64); NornicDB: `workers` | Number of reducer intents claimed per polling cycle. |
| **`ESHU_REDUCER_SEMANTIC_ENTITY_CLAIM_LIMIT`** | NornicDB: `1`; otherwise disabled | Concurrent semantic entity materialization claims after source-local drain. |
| **`ESHU_REDUCER_CLAIM_DOMAIN`** | unset | Optional domain filter for split-reducer diagnostics, for example `sql_relationship_materialization` or `deployment_mapping`. Leave unset for the normal all-domain reducer. |
| **`ESHU_CODE_CALL_PROJECTION_ACCEPTANCE_SCAN_LIMIT`** | `250000` | Maximum pending code-call shared intents the reducer may scan or load for one accepted repo/run before failing safely instead of rewriting CALLS edges from a partial slice. |
| **`ESHU_SHARED_PROJECTION_WORKERS`** | `1` | Concurrent shared-projection partition workers. |
| **`ESHU_SHARED_PROJECTION_PARTITION_COUNT`** | `8` | Number of shared-projection partitions per domain. |
| **`ESHU_SHARED_PROJECTION_BATCH_LIMIT`** | `100` | Maximum intents processed per shared-projection partition batch. |
| **`ESHU_SHARED_PROJECTION_POLL_INTERVAL`** | `5s` | Idle poll interval for shared projection work. |
| **`ESHU_SHARED_PROJECTION_LEASE_TTL`** | `60s` | Lease duration for shared-projection partition claims. |

Unsupported runtime controls:

- `ESHU_REPO_FILE_PARSE_MULTIPROCESS`
- `ESHU_MULTIPROCESS_START_METHOD`
- `ESHU_WORKER_MAX_TASKS`
- `ESHU_INDEX_QUEUE_DEPTH`
- `ESHU_WATCH_DEBOUNCE_SECONDS`

Raise `ESHU_CODE_CALL_PROJECTION_ACCEPTANCE_SCAN_LIMIT` only for the explicit
code-call acceptance-cap failure. It is not a generic queue-throughput or
graph-timeout knob; capture a discovery advisory first and prefer filtering
generated/vendor/archive source when that is what inflated the code-call slice.

`eshu index` launches the Go `bootstrap-index` runtime in direct filesystem
mode, and `eshu watch` hands off to the Go ingester runtime. Neither command
uses these unsupported controls.

### Indexing Scope

The current Go runtime does not expose public environment variables for
file-size limits, hidden-directory skipping, or dependency-root pruning. Those
defaults are built into the discovery and parser pipeline.

Use `.eshu/discovery.json` for repo-specific, reasoned exclusions that should be
visible in discovery logs and metrics. Eshu still accepts the older
`.eshu/vendor-roots.json` shape as a compatibility alias, but new repo-specific
tuning should use `.eshu/discovery.json`.
When deciding whether to add a rule, capture a discovery advisory report first
and validate the after-run with the
[Local Testing — Discovery Advisory Playbook](local-testing.md#discovery-advisory-playbook).

Example:

```json
{
  "ignored_path_globs": [
    {"path": "src/wp-content/plugins/wordpress-seo/**", "reason": "wordpress-seo"}
  ],
  "preserved_path_globs": [
    {"path": "src/wp-content/plugins/custom-authored/**"}
  ]
}
```

`ignored_path_globs[].path` and `preserved_path_globs[].path` are
repository-relative globs. Patterns ending in `/**` prune a subtree. `reason`
is emitted in discovery logs and metrics as `user:<reason>` so operators can
verify that a noisy repo became cheaper for the intended reason. Prefer exact
third-party roots over broad ecosystem parents such as `wp-content/plugins/**`
unless a preserve rule keeps the authored subtrees you need.

The legacy `.eshu/vendor-roots.json` file uses `vendor_roots` and `keep_roots`
with the same path and reason semantics. If both files are present, Eshu merges
both configurations and de-duplicates identical rules.

Use the repository-source settings below when you need to switch between GitHub
discovery, explicit repo lists, and direct filesystem mode.

### Database Connection (Neo4j)

| Key | Description |
| :--- | :--- |
| **`NEO4J_URI`** | Connection URI (e.g., `bolt://localhost:7687`). |
| **`NEO4J_USERNAME`** | Database user (default: `neo4j`). |
| **`NEO4J_PASSWORD`** | Database password. |

### Go Runtime Database Tuning

These settings are consumed by the Go runtime data plane and are especially
useful for split-service Kubernetes deployments where API, ingester, and
resolution-engine workloads should be tuned independently.

| Key | Default | Description |
| :--- | :--- | :--- |
| **`ESHU_POSTGRES_MAX_OPEN_CONNS`** | runtime default | Maximum open PostgreSQL connections for Go runtimes. |
| **`ESHU_POSTGRES_MAX_IDLE_CONNS`** | runtime default | Maximum idle PostgreSQL connections for Go runtimes. |
| **`ESHU_POSTGRES_CONN_MAX_LIFETIME`** | runtime default | Maximum lifetime for one PostgreSQL connection. |
| **`ESHU_POSTGRES_CONN_MAX_IDLE_TIME`** | runtime default | Maximum idle lifetime for one PostgreSQL connection. |
| **`ESHU_POSTGRES_PING_TIMEOUT`** | runtime default | Timeout used when a Go runtime verifies PostgreSQL connectivity during startup. |
| **`ESHU_NEO4J_MAX_CONNECTION_POOL_SIZE`** | runtime default | Maximum Neo4j driver pool size for Go runtimes. |
| **`ESHU_NEO4J_MAX_CONNECTION_LIFETIME`** | runtime default | Maximum lifetime for one Neo4j driver connection. |
| **`ESHU_NEO4J_CONNECTION_ACQUISITION_TIMEOUT`** | runtime default | Timeout while waiting for a Neo4j pooled connection. |
| **`ESHU_NEO4J_SOCKET_CONNECT_TIMEOUT`** | runtime default | Timeout for establishing a Neo4j socket connection. |
| **`ESHU_NEO4J_VERIFY_TIMEOUT`** | runtime default | Timeout used when a Go runtime verifies Neo4j connectivity during startup. |

### Content Store And Source Retrieval

| Key | Default | Description |
| :--- | :--- | :--- |
| **`ESHU_CONTENT_STORE_DSN`** | unset | Primary DSN for the PostgreSQL content store. |
| **`ESHU_POSTGRES_DSN`** | unset | Backward-compatible alias for the PostgreSQL content store DSN. |
| **`ESHU_FACT_STORE_DSN`** | unset | Primary DSN for the facts-first PostgreSQL fact store. Falls back to `ESHU_CONTENT_STORE_DSN` or `ESHU_POSTGRES_DSN` when unset. |

Notes:

- deployed API runtimes use the PostgreSQL content store directly and return `unavailable` when content is not yet indexed
- facts-first Git ingestion also uses Postgres for fact persistence and queued projection work
- local helper flows may still fall back to the workspace or graph cache
- content search routes and MCP search tools require PostgreSQL-backed content to be available
- portable source retrieval uses `repo_id + relative_path` for files and `entity_id` for content-bearing entities
- the Helm chart exposes Go runtime pool tuning per workload under `api.connectionTuning`, `ingester.connectionTuning`, and `resolutionEngine.connectionTuning`

### Ingester Runtime

These settings matter for deployable-service installs that use the repository ingester runtime.

| Key | Default | Description |
| :--- | :--- | :--- |
| **`ESHU_REPO_SOURCE_MODE`** | `githubOrg` | Repository discovery mode. Supported modes include `githubOrg`, `explicit`, and `filesystem`. |
| **`ESHU_GITHUB_ORG`** | unset | GitHub organization used for repository discovery in `githubOrg` mode. |
| **`ESHU_REPOSITORY_RULES_JSON`** | unset | Structured exact/regex include rules applied to normalized `org/repo` identifiers during repo rediscovery. Exact rules also define repository IDs for `explicit` and `filesystem` source modes. |
| **`ESHU_REPOS_DIR`** | `/data/repos` | Shared workspace directory for cloned repositories. |
| **`ESHU_REPO_LIMIT`** | `4000` | Maximum repositories to discover from GitHub in one cycle. |

`ESHU_REPOSITORY_RULES_JSON` accepts either a list of rules or an object with `exact` and `regex` keys. Example:

```json
[
  {"exact": "eshu-hq/eshu"},
  {"regex": "eshu-hq/(payments|orders)-.*"}
]
```

The repository ingester re-discovers repositories on each cycle, applies these rules, updates matching checkouts, and reports stale local checkouts that no longer match the discovery result.

---

## Configuration Files

Eshu uses the following hierarchy:

1.  **Project Level:** `.eshuignore` and `.eshu/discovery.json` in your project
    root (repo-local indexing exclusions).
2.  **User Level:** `~/.eshu/.env` (global settings).
3.  **Defaults:** Built-in application defaults.

That user-level `.env` file is for CLI configuration. It is not the local API
bearer-token store; the Go API and MCP runtimes read `ESHU_API_KEY` from their
own process environment when bearer auth is enabled.

Use `.eshuignore` for plain gitignore-style project exclusions. Use
`.eshu/discovery.json` when you need reasoned pruning with `user:<reason>`
telemetry. Use the built-in dependency-root pruning plus repository-source
settings for global indexing behavior.

For logging, the rule is simpler: the current Go runtimes always emit JSON to
stderr, so deployment tuning should focus on OTLP export and log shipping
rather than a runtime log-format switch.

To reset everything to defaults:
```bash
eshu config reset
```

## `eshu workspace` Commands

Use the workspace command group when you want local CLI behavior to follow the same
repository-source contract as the cloud ingester.

```bash
eshu workspace plan
eshu workspace sync
eshu workspace index
eshu workspace status
eshu workspace watch
```

- `plan` previews the repositories selected by the current source configuration
- `sync` materializes the matching repositories into `ESHU_REPOS_DIR` without starting a manual index run
- `index` launches the Go `bootstrap-index` runtime against the configured
  workspace using direct filesystem mode, so local workspace indexing follows
  the same parser and write path as the deployed data plane
- `status` reports the configured workspace path plus the latest checkpointed workspace index summary
- `watch` watches the materialized workspace in repo-partitioned mode and can optionally rediscover newly added repos with `--sync-interval-seconds`

Path-first commands such as `eshu index <path>` and `eshu watch <path>` still work as
local filesystem convenience wrappers. `eshu index` now shells into the Go
`bootstrap-index` runtime with a persistent state directory under `ESHU_HOME`,
while `eshu watch` remains the local incremental convenience surface. They do not
replace the canonical workspace source model.

## Recovery Commands

Recovery surfaces are part of the Go runtime and API admin contracts.

- runtime-local recovery mounts are exposed by the ingester at
  `/admin/refinalize` and `/admin/replay`
- API-admin recovery routes are exposed under `/api/v0/admin/*`
- `eshu workspace index` and `eshu workspace watch` remain valuable for local
  development and workstation workflows, while the deployed write plane stays
  split across `ingester` and `resolution-engine`

If you are tuning or operating a deployed environment, start with the runtime
and queue settings for the service you are scaling. Use the repair commands
only when you are intentionally replaying or recovering already-collected data.
