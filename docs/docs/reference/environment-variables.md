# Environment Variables

This is the operator reference for Eshu environment variables. Use it to answer
three questions before changing a value:

- What runtime reads this variable?
- What default does Eshu use when it is unset?
- What symptom justifies tuning it?

Tune from evidence, not from vibes. A queue backlog, by itself, is not proof
that a worker count should go up; a timeout, by itself, is not proof that the
timeout should be longer. First identify the stage, phase, label, row count,
claim age, and latest failure class from the status panel, logs, or discovery
advisory report.

## Tuning Rules

| Rule | Why |
| --- | --- |
| Change the narrowest knob that names the failing stage. | Broad knobs hide root cause and make later regressions harder to bisect. |
| Prefer filtering generated/vendor input before increasing graph-write budgets. | Bigger batches of wrong input still produce wrong or noisy graph truth. |
| Keep claim windows near worker count for slow backends. | Pre-claiming more work than workers can start causes lease expiry and duplicate work. |
| Increase timeouts only after the statement shape is proven correct. | A longer timeout can hide missing indexes, bad query routing, or row-shape bugs. |
| Record the before/after evidence in the active ADR for runtime-profile work. | Performance tuning without provenance turns into folklore. |

## Core Runtime

| Variable | Default | Read By | Purpose | Tune When |
| --- | --- | --- | --- | --- |
| `ESHU_HOME` | Platform user-data dir | CLI, local Eshu service, API key resolver | Root for user config, local workspaces, managed binaries, and persisted local API keys. | Set to isolate dogfood runs, CI runs, or disposable local-authoritative workspaces. |
| `ESHU_QUERY_PROFILE` | `production` for API/MCP/reducer; local commands set profile explicitly | API, MCP, ingester, reducer, local Eshu service | Selects query/runtime profile such as `production`, `local_lightweight`, or `local_authoritative`. | Change only when switching runtime mode. Do not use it as a performance knob. |
| `ESHU_GRAPH_BACKEND` | `nornicdb` | API, MCP, ingester, reducer, local Eshu service | Selects graph adapter: `nornicdb` or `neo4j`. | Set to `neo4j` for the explicit Neo4j path. |
| `ESHU_LISTEN_ADDR` | `0.0.0.0:8080` | Go service runtimes | HTTP listen address for services using shared runtime config. | Change for deployment port binding, not performance. |
| `ESHU_METRICS_ADDR` | `0.0.0.0:9464` | Go service runtimes | Prometheus metrics listen address. | Change for deployment port binding or sidecar scrape layout. |
| `ESHU_PPROF_ADDR` | unset (pprof disabled) | API, MCP, ingester, reducer, bootstrap-index | Opt-in `net/http/pprof` endpoint. A bare port (e.g. `:6060`) binds to `127.0.0.1`; supply an explicit host to expose elsewhere. | Set when you need a CPU, heap, or goroutine profile and have evidence the next bottleneck is on a specific binary. Leave unset in deployment defaults. |
| `ESHU_API_ADDR` | unset; CLI wrappers set host/port flags | API CLI service wrapper | API listen address when using `eshu service` helpers. | Use CLI flags first; set only for scripted local service wrappers. |
| `ESHU_MCP_TRANSPORT` | `http` | MCP server | MCP transport: `http` or `stdio`. | Set to `stdio` only for stdio MCP clients or local attach flows. |
| `ESHU_MCP_ADDR` | `:8080` | MCP server | HTTP MCP listen address. | Change for deployment port binding. |
| `ESHU_RUNTIME_DB_TYPE` | CLI flag derived | CLI root command | Legacy CLI database selector written from global flags. | Do not tune directly. Prefer explicit graph/backend settings. |
| `ESHU_WATCH_PATH` | local Eshu service sets it | local child processes | Workspace path handed to local child processes. | Internal; do not set manually. |
| `ESHU_DISABLE_NEO4J` | unset | API, MCP, ingester, local Eshu service | Transitional local-lightweight skip flag. | Internal compatibility flag; prefer `ESHU_QUERY_PROFILE` and `ESHU_GRAPH_BACKEND`. |

## Authentication And Remote CLI

| Variable | Default | Read By | Purpose | Tune When |
| --- | --- | --- | --- | --- |
| `ESHU_API_KEY` | unset, or generated/persisted when allowed | API, MCP, CLI | Bearer token for local/API calls. | Set in deployment secrets or local client config. Rotate like a credential. |
| `ESHU_AUTO_GENERATE_API_KEY` | `false` | API/MCP runtime key resolver | Allows local runtimes to generate and persist an API key under `ESHU_HOME`. | Use for local compose/dev convenience only. |
| `ESHU_SERVICE_URL` | unset | CLI | Default remote Eshu API base URL. | Set for CLI-to-service workflows. |
| `ESHU_SERVICE_PROFILE` | unset | CLI | Selects profile-specific `ESHU_SERVICE_URL_<PROFILE>` and `ESHU_API_KEY_<PROFILE>`. | Use when switching between QA/stage/prod services. |
| `ESHU_SERVICE_URL_<PROFILE>` | unset | CLI | Profile-specific service URL. | Use for named remote environments. |
| `ESHU_API_KEY_<PROFILE>` | unset | CLI | Profile-specific API key. | Use for named remote environments. |
| `ESHU_REMOTE_TIMEOUT_SECONDS` | `30` | CLI HTTP client | Timeout for remote CLI requests. | Raise for legitimately long read queries over slow links; do not raise for hung services without checking server logs. |

## Postgres

| Variable | Default | Read By | Purpose | Tune When |
| --- | --- | --- | --- | --- |
| `ESHU_FACT_STORE_DSN` | unset | Go runtimes | Primary DSN for fact store and queues. | Required in deployed facts-first runtimes unless falling back to content/Postgres DSN. |
| `ESHU_CONTENT_STORE_DSN` | unset | API, MCP, ingester, reducer | DSN for content store and query surfaces. | Required when content/search/query surfaces read Postgres. |
| `ESHU_POSTGRES_DSN` | unset | Go runtimes, CLI doctor | Backward-compatible Postgres DSN fallback. | Prefer explicit fact/content DSNs in split-service deployments. |
| `ESHU_POSTGRES_MAX_OPEN_CONNS` | `30` | Go runtimes | Maximum open Postgres connections per process. | Raise only when Postgres wait time is visible and the database has capacity; lower when many pods exhaust Postgres connections. |
| `ESHU_POSTGRES_MAX_IDLE_CONNS` | `10` | Go runtimes | Maximum idle Postgres connections. | Tune with max-open conns; keep at or below max-open. |
| `ESHU_POSTGRES_CONN_MAX_LIFETIME` | `30m` | Go runtimes | Maximum lifetime of one Postgres connection. | Lower when load balancers or proxies recycle connections sooner. |
| `ESHU_POSTGRES_CONN_MAX_IDLE_TIME` | `10m` | Go runtimes | Maximum idle lifetime of one Postgres connection. | Lower for constrained DB pools; raise rarely. |
| `ESHU_POSTGRES_PING_TIMEOUT` | `10s` | Go runtimes | Startup ping timeout. | Raise only for slow network startup; a running service should not depend on a long ping. |
| `ESHU_CONTENT_ENTITY_BATCH_SIZE` | `300` | bootstrap-index, ingester/projector, projector | Rows per Postgres `content_entities` upsert statement. Values must be `1..4000`. | Tune only after `content writer stage completed` logs show `upsert_entities` dominates and `prepare_entities` is cheap. Raise or lower on one focused repo first; if statement count changes but wall time does not, inspect `source_cache` size and trigram index cost instead of continuing to raise the batch. Do not use this for graph-write timeouts. |
| `ESHU_LOCAL_AUTHORITATIVE_DEFER_CONTENT_SEARCH_INDEXES` | unset / `false` | `eshu graph start` local Eshu service | Skips the two expensive content trigram search indexes during local-authoritative bootstrap, then creates them after the discovered filesystem repo set reaches a clean projector/reducer/shared-intent drain. | Use only for local-authoritative bulk-load proofs where `content writer stage completed` shows trigram maintenance dominates content writes. It preserves rows and restores search indexes after drain; do not use it as a deployed Postgres schema default. |
| `ESHU_DISCOVERY_IGNORED_PATH_GLOBS` | unset | bootstrap-index, collector-git, ingester | Comma- or newline-separated repo-relative discovery globs. Entries may use `pattern=reason`; the default reason is `env-ignore`. | Operator-controlled overlay for focused generated/vendor/archive input-shape proofs. Prefer repo-local `.eshu/discovery.json` for durable source-owned rules. |
| `ESHU_DISCOVERY_PRESERVED_PATH_GLOBS` | unset | bootstrap-index, collector-git, ingester | Comma- or newline-separated repo-relative globs to keep when a broader ignored glob covers an ancestor. | Pair with broad ignored globs so authored subtrees stay indexable. |

## Graph Driver

| Variable | Default | Read By | Purpose | Tune When |
| --- | --- | --- | --- | --- |
| `ESHU_NEO4J_URI` / `NEO4J_URI` | unset | API, MCP, ingester, reducer, CLI doctor | Bolt URI for NornicDB or Neo4j. ESHU-prefixed value wins. | Required for graph-backed services. |
| `ESHU_NEO4J_USERNAME` / `NEO4J_USERNAME` | unset | Graph runtimes | Bolt auth username. | Set from deployment secrets. |
| `ESHU_NEO4J_PASSWORD` / `NEO4J_PASSWORD` | unset | Graph runtimes | Bolt auth password. | Set from deployment secrets. |
| `ESHU_NEO4J_DATABASE` / `NEO4J_DATABASE` | `nornic` for default stacks | Graph runtimes | Bolt database name. | Change when switching between NornicDB (`nornic`) and Neo4j (`neo4j`) or for explicit multi-database deployments. |
| `DEFAULT_DATABASE` | `nornic` for default stacks | API graph open path, CLI config | Legacy/default Bolt database name. | Prefer `ESHU_NEO4J_DATABASE`; keep for legacy config compatibility. |
| `ESHU_NEO4J_BATCH_SIZE` | `500` | ingester, reducer, projector, bootstrap-index | Generic graph UNWIND row batch size. | Lower only when Neo4j/NornicDB statement row width is the proven bottleneck; prefer label/phase-specific NornicDB knobs where available. |
| `ESHU_NEO4J_MAX_CONNECTION_POOL_SIZE` | `100` | Graph runtimes | Max driver connections. | Raise when driver acquisition waits and DB capacity exists; lower when too many service pods oversubscribe Neo4j. |
| `ESHU_NEO4J_MAX_CONNECTION_LIFETIME` | `1h` | Graph runtimes | Max driver connection lifetime. | Lower for proxies/load balancers that recycle connections. |
| `ESHU_NEO4J_CONNECTION_ACQUISITION_TIMEOUT` | `1m` | Graph runtimes | Time waiting for a driver connection. | Raise only if pool pressure is expected and bounded; otherwise fix pool sizing or query latency. |
| `ESHU_NEO4J_SOCKET_CONNECT_TIMEOUT` | `5s` | Graph runtimes | Socket connect timeout. | Raise for slow networks; not a graph-write tuning knob. |
| `ESHU_NEO4J_VERIFY_TIMEOUT` | `10s` | Graph runtimes | Startup verification timeout. | Raise only for slow backend startup. |
| `ESHU_NEO4J_PROFILE_GROUP_STATEMENTS` | `false` | ingester, bootstrap-index | Logs per-statement attempt timing inside Neo4j grouped writes, including phase, label, row count, and statement summary when available. | Enable only during Neo4j write-shape investigations. It is noisy and its logs describe transaction attempts, not final commit proof. |

## Repository Discovery And Parsing

| Variable | Default | Read By | Purpose | Tune When |
| --- | --- | --- | --- | --- |
| `ESHU_REPO_SOURCE_MODE` | `githubOrg` | collector, ingester, bootstrap-index | Repository source mode: `githubOrg`, `explicit`, or `filesystem`. | Change when switching between org sync, explicit repo list, and local filesystem indexing. |
| `ESHU_GITHUB_ORG` | unset | GitHub selector | GitHub organization to discover. | Required for `githubOrg` mode. |
| `ESHU_REPOSITORY_RULES_JSON` | unset | collector selector | Exact/regex include rules; exact rules define repos for `explicit` and `filesystem`. | Use to scope org scans or define a controlled repo subset. |
| `ESHU_REPOS_DIR` | `/data/repos` | collector | Local clone/cache directory. | Put on fast persistent disk; for local-authoritative it is set under workspace cache. |
| `ESHU_REPO_LIMIT` | `4000` | GitHub selector | Maximum repos discovered in one cycle. | Raise only for orgs larger than the default and after checking GitHub rate limits. |
| `ESHU_CLONE_DEPTH` | `1` | collector | Git clone depth. | Raise only if analysis needs history-sensitive files not present in shallow clone. |
| `ESHU_GIT_AUTH_METHOD` | `githubApp` | collector | Git auth mode: GitHub App, token, or SSH path depending on source. | Change to match deployment credentials. |
| `ESHU_GIT_TOKEN` / `GITHUB_TOKEN` | unset | collector | Token auth credential. | Use for token auth; prefer secrets manager in deployments. |
| `ESHU_GITHUB_APP_ID` / `GITHUB_APP_ID` | unset | collector | GitHub App ID. | Required for GitHub App auth. |
| `ESHU_GITHUB_APP_INSTALLATION_ID` / `GITHUB_APP_INSTALLATION_ID` | unset | collector | GitHub App installation ID. | Required for GitHub App auth. |
| `ESHU_GITHUB_APP_PRIVATE_KEY` / `GITHUB_APP_PRIVATE_KEY` | unset | collector | GitHub App private key. | Required for GitHub App auth; handle as secret. |
| `ESHU_SSH_PRIVATE_KEY_PATH` | unset | collector | SSH key path for SSH clone auth. | Use for SSH auth mode. |
| `ESHU_SSH_KNOWN_HOSTS_PATH` | unset | collector | Known-hosts path for SSH clone verification. | Use with SSH auth in locked-down environments. |
| `ESHU_INCLUDE_ARCHIVED_REPOS` | `false` | GitHub selector | Include archived repos. | Enable only when archived repos are intentionally part of the graph. |
| `ESHU_FILESYSTEM_ROOT` | unset | filesystem selector | Root path for filesystem source mode. | Required for direct local indexing outside local-host wrappers. |
| `ESHU_FILESYSTEM_DIRECT` | `false` | filesystem selector | Treat filesystem root as direct source rather than cloned cache flow. | Local/direct indexing only. |
| `ESHU_SNAPSHOT_WORKERS` | `min(NumCPU, 8)`; local-authoritative owner: `NumCPU` | collector | Concurrent repo snapshot workers. | Raise when CPU/disk/network have headroom and many small repos wait; lower when memory or disk I/O saturates. |
| `ESHU_PARSE_WORKERS` | `min(NumCPU, 8)`; local-authoritative owner: `NumCPU` | collector snapshotter | Concurrent file parse workers inside each snapshot. | Raise for CPU headroom; lower when parser memory or disk reads saturate. |
| `ESHU_STREAM_BUFFER` | `0` | collector | Generation stream buffer; `0` derives from worker count. | Rarely tune; raise only when profiling shows producer/consumer handoff idle. |
| `ESHU_LARGE_REPO_FILE_THRESHOLD` | `1000` | collector | File-count threshold for large-repo semaphore. | Lower when medium repos cause memory spikes; raise when many medium repos are being over-throttled. |
| `ESHU_LARGE_REPO_MAX_CONCURRENT` | `2` | collector | Concurrent large repo snapshots. | Lower for memory stability; raise on high-memory machines after advisory reports show large repos are the bottleneck. |
| `ESHU_DISCOVERY_REPORT` | unset | `eshu index`, bootstrap-index | Writes per-repo discovery advisory JSON. | Set before changing ignore/vendor rules or raising caps so the input shape is evidence-backed. |
| `ESHU_BOOTSTRAP_IS_DEPENDENCY` | `false` | collector | Marks bootstrap source as dependency package. | Specialized dependency ingestion only. |
| `ESHU_BOOTSTRAP_PACKAGE_NAME` | unset | collector | Dependency package name. | Use with dependency bootstrap mode. |
| `ESHU_BOOTSTRAP_PACKAGE_LANGUAGE` | unset | collector | Dependency package language. | Use with dependency bootstrap mode. |
| `SCIP_INDEXER` | `false` | collector snapshotter | Enables SCIP supplement indexing. | Enable only when SCIP tooling is installed and semantic supplement output is required. |
| `SCIP_LANGUAGES` | unset | collector snapshotter | Comma-separated SCIP language selection. | Narrow SCIP work to known supported languages. |

## Projection And Reducer Queues

| Variable | Default | Read By | Purpose | Tune When |
| --- | --- | --- | --- | --- |
| `ESHU_PROJECTOR_WORKERS` | Default: `min(NumCPU, 8)`; NornicDB local-authoritative: `NumCPU` | ingester projector | Source-local projector worker count. | Lower when graph-write telemetry shows backend saturation or conflict-key contention; explicit env vars override the local owner. |
| `ESHU_LARGE_GEN_THRESHOLD` | `10000` facts | ingester projector | Fact-count threshold for large-generation semaphore. | Lower when medium generations cause memory/write spikes; raise if safe repos are over-throttled. |
| `ESHU_LARGE_GEN_MAX_CONCURRENT` | Default: `2`; local-authoritative: `4` | ingester projector | Concurrent large source-local generations. | Lower for NornicDB/write stability; raise only with graph-write headroom. |
| `ESHU_PROJECTOR_MAX_ATTEMPTS` | `3` | ingester/projector retry policy | Max projector attempts before terminal failure. | Graph write timeouts and transient backend conflicts use this bounded budget; raise for correctness-validation lanes only after deterministic write-shape bugs are ruled out. |
| `ESHU_PROJECTOR_RETRY_DELAY` | `30s` | ingester/projector retry policy | Delay between projector retries. | Tune with backend recovery time; keep short enough to surface permanent failures. |
| `ESHU_PROJECTOR_RETRY_ONCE_SCOPE_GENERATION` | unset | projector runtime | Test/fault-injection retry hook for one scope generation. | Internal verification only. |
| `ESHU_PROJECTION_WORKERS` | `min(NumCPU, 8)` | bootstrap-index | Bootstrap projection worker count. | Same guidance as projector workers; tune after observing projection queue latency and graph-write health. |
| `ESHU_REDUCER_WORKERS` | Neo4j: `min(NumCPU, 4)`; NornicDB: `NumCPU` | reducer | Reducer intent worker count. | Lower only when conflict-domain fencing still shows graph write conflicts or backend saturation. Raise explicitly only after measuring queue tail, CPU, disk, and graph-write timeout rates. |
| `ESHU_REDUCER_BATCH_CLAIM_SIZE` | Neo4j: `workers*4` capped `4..64`; NornicDB: `workers` | reducer | Number of reducer intents claimed per poll. | Keep near worker count for slow graph backends so every claimed item can start heartbeat-protected execution promptly. |
| `ESHU_REDUCER_SEMANTIC_ENTITY_CLAIM_LIMIT` | NornicDB: `1`; otherwise disabled | reducer | Concurrent semantic entity materialization claims allowed after the source-local drain gate opens. | Raise only in focused NornicDB proofs after semantic graph writes are measured as bounded; keep low if tiny semantic batches timeout or retry. |
| `ESHU_REDUCER_CLAIM_DOMAIN` | unset | reducer | Restricts the main reducer claim loop to one reducer domain such as `sql_relationship_materialization` or `deployment_mapping`. Shared projection side runners still use their own lease controls. | Use for domain-lane diagnostics or controlled split-reducer experiments after proving graph truth on a focused corpus. Do not use it to hide stuck work; run an all-domain reducer or matching domain lanes for every required domain. |
| `ESHU_REDUCER_MAX_ATTEMPTS` | `3` | reducer retry policy | Max reducer attempts before terminal failure. | Graph write timeouts and transient backend conflicts use this bounded budget; raise for correctness-validation lanes only after deterministic write-shape bugs are ruled out. |
| `ESHU_REDUCER_RETRY_DELAY` | `30s` | reducer retry policy | Delay between reducer retries. | Tune to backend recovery time, not to mask deterministic failures. |
| `ESHU_SHARED_PROJECTION_WORKERS` | `1` | reducer shared projection | Partition worker count for shared projection domains. | Raise when shared-projection queue age grows and graph write backend is healthy. |
| `ESHU_SHARED_PROJECTION_PARTITION_COUNT` | `8` | reducer shared projection | Partitions per shared domain. | Raise only if workers need more independent partitions; changing this affects partition distribution. |
| `ESHU_SHARED_PROJECTION_BATCH_LIMIT` | `100` | reducer shared projection | Intents per partition batch. | Lower if lease duration is exceeded; raise if each batch is tiny and backend is healthy. |
| `ESHU_SHARED_PROJECTION_POLL_INTERVAL` | `5s` | reducer shared projection | Idle poll interval. | Lower for responsiveness; raise to reduce idle DB polling. |
| `ESHU_SHARED_PROJECTION_LEASE_TTL` | `60s` | reducer shared projection | Partition lease TTL. | Raise when legitimate batches approach TTL; lower only if failed workers need faster takeover. |
| `ESHU_CODE_CALL_PROJECTION_POLL_INTERVAL` | `500ms` | reducer code-call sidecar | Idle poll interval for code-call projection. | Lower for responsiveness; raise to reduce idle polling. |
| `ESHU_CODE_CALL_PROJECTION_LEASE_TTL` | `60s` | reducer code-call sidecar | Lease TTL for code-call projection work. | Raise only when a complete accepted repo/run normally exceeds TTL. |
| `ESHU_CODE_CALL_PROJECTION_BATCH_LIMIT` | `100` | reducer code-call sidecar | Claim batch size for code-call work. | Rarely tune; use latest failure and duration logs first. |
| `ESHU_CODE_CALL_PROJECTION_ACCEPTANCE_SCAN_LIMIT` | `250000` | reducer code-call sidecar | Correctness guard for complete accepted repo/run scan before rewriting CALLS edges. | Increase only for explicit acceptance-cap failures and after a discovery advisory proves the volume is authored source. |
| `ESHU_CODE_CALL_PROJECTION_LEASE_OWNER` | `code-call-projection-runner` | reducer code-call sidecar | Lease owner name. | Internal/multi-run diagnostics only. |
| `ESHU_REPO_DEPENDENCY_PROJECTION_POLL_INTERVAL` | `500ms` | reducer repo-dependency sidecar | Idle poll interval. | Same sidecar polling guidance as code-call projection. |
| `ESHU_REPO_DEPENDENCY_PROJECTION_LEASE_TTL` | `60s` | reducer repo-dependency sidecar | Lease TTL. | Raise only for legitimate long dependency projection batches. |
| `ESHU_REPO_DEPENDENCY_PROJECTION_BATCH_LIMIT` | `100` | reducer repo-dependency sidecar | Claim batch size. | Tune only from sidecar duration/lease evidence. |
| `ESHU_REPO_DEPENDENCY_PROJECTION_LEASE_OWNER` | `repo-dependency-projection-runner` | reducer repo-dependency sidecar | Lease owner name. | Internal/multi-run diagnostics only. |
| `ESHU_GRAPH_PROJECTION_REPAIR_POLL_INTERVAL` | `1s` | reducer repairer | Poll interval for graph projection phase repair. | Lower for faster repair pickup; raise to reduce idle polling. |
| `ESHU_GRAPH_PROJECTION_REPAIR_BATCH_LIMIT` | `100` | reducer repairer | Repair rows per batch. | Lower if repair batches exceed leases/timeouts; raise if repair is healthy but too chatty. |
| `ESHU_GRAPH_PROJECTION_REPAIR_RETRY_DELAY` | `1m` | reducer repairer | Delay before retrying repair. | Tune to backend recovery characteristics. |

## Graph Write Shape And NornicDB

| Variable | Default | Read By | Purpose | Tune When |
| --- | --- | --- | --- | --- |
| `ESHU_CANONICAL_WRITE_TIMEOUT` | `30s` on NornicDB | ingester, reducer graph writers | Client context and Bolt transaction timeout for NornicDB writes. | Raise for focused correctness-validation lanes when the only blocker is a bounded graph deadline; keep statement summaries and later tune write shape/concurrency instead of treating a larger timeout as the final perf answer. |
| `ESHU_NORNICDB_PHASE_GROUP_STATEMENTS` | `500` | graph writer | Broad grouped statement cap for phases without a narrower cap. | Tune only when timeout summaries name a broad phase without phase-specific controls. |
| `ESHU_NORNICDB_FILE_PHASE_GROUP_STATEMENTS` | `5` | graph writer | Grouped statement cap for `phase=files`. | Lower when file-phase grouped execution times out; raise only after file chunks are consistently fast. |
| `ESHU_NORNICDB_FILE_BATCH_SIZE` | `100` | graph writer | Rows per file-upsert statement. | Lower when one file statement is too wide; do not use for entity or reducer semantic failures. |
| `ESHU_NORNICDB_ENTITY_PHASE_GROUP_STATEMENTS` | `25` | graph writer | Grouped statement cap for canonical entity phases. | Tune when entity phase group width, not row width, is the proven bottleneck. |
| `ESHU_NORNICDB_ENTITY_BATCH_SIZE` | `100` | graph writer | Default rows per canonical entity statement. | Lower only when many labels are too wide; prefer label-specific caps first. |
| `ESHU_NORNICDB_ENTITY_LABEL_BATCH_SIZES` | `Function=15,K8sResource=1,Struct=50,Variable=100` | graph writer | Label-specific canonical entity row caps. | Add/narrow labels when timeout logs name that label and row count. |
| `ESHU_NORNICDB_ENTITY_LABEL_PHASE_GROUP_STATEMENTS` | `Function=5,K8sResource=1,Struct=15,Variable=5` | graph writer | Label-specific grouped statement caps. | Tune when row size is healthy but grouped execution of that label drifts upward. |
| `ESHU_NORNICDB_ENTITY_PHASE_CONCURRENCY` | `NumCPU` clamped to `16` | graph writer | Worker count for parallel canonical entity-phase chunk dispatch. | Set to `1` for the serial path; pin lower than the CPU-derived default only when NornicDB shows contention on parallel commits rather than headroom. Both the default and env override clamp to `16` to bound Bolt session demand. |
| `ESHU_NORNICDB_SEMANTIC_ENTITY_LABEL_BATCH_SIZES` | `Annotation=5,Function=10,ImplBlock=10,Module=10,TypeAlias=5,TypeAnnotation=50,Variable=10` | reducer semantic writer | Label-specific semantic entity row caps. | Tune only for semantic materialization timeout summaries naming that label. |
| `ESHU_CODE_CALL_EDGE_BATCH_SIZE` | `1000` | reducer code-call edge writer | Rows per code-call edge write statement. | Lower if code-call edge write statements time out after acceptance scan succeeds. |
| `ESHU_CODE_CALL_EDGE_GROUP_BATCH_SIZE` | `1` | reducer code-call edge writer | Statements per grouped code-call edge execution. | Raise only after backend proves grouped code-call edge writes are safe and faster. |
| `ESHU_INHERITANCE_EDGE_GROUP_BATCH_SIZE` | `1` | reducer shared edge writer | Grouped statements for inheritance edges. | Raise only after edge writes are stable and graph backend can absorb grouping. |
| `ESHU_SQL_RELATIONSHIP_EDGE_GROUP_BATCH_SIZE` | `1` | reducer shared edge writer | Grouped statements for SQL relationship edges. | Same guidance as inheritance edge grouping. |
| `ESHU_NORNICDB_CANONICAL_GROUPED_WRITES` | `false` | graph writer | Conformance switch for Neo4j-style grouped writes on NornicDB. | Test/conformance only. Leave unset for normal laptop runs. |
| `ESHU_NORNICDB_REQUIRE_GROUPED_ROLLBACK` | `false` | NornicDB tests | Makes grouped rollback conformance mandatory. | Test gate only. |
| `ESHU_NORNICDB_BATCHED_ENTITY_CONTAINMENT` | `false` | graph writer | Evaluation switch for cross-file batched containment. | Use only with a latest-main NornicDB binary that has focused proof for the repo shape under test. |
| `ESHU_NORNICDB_RUNTIME` | `embedded` | local Eshu service | Selects the local NornicDB runtime: `embedded` or `process`. | Leave unset for normal local mode. Set `process` only when testing an external NornicDB binary. |
| `ESHU_NORNICDB_BINARY` | unset | local Eshu service, install, tests | Explicit process-mode NornicDB binary path. | Use with `ESHU_NORNICDB_RUNTIME=process` to test a latest-main binary directly or temporarily override an older managed install. |
| `ESHU_NORNICDB_INSTALL_TIMEOUT` | `30s` | `eshu install nornicdb` | Download timeout for installer sources. | Raise for slow artifact downloads. |
| `NORNICDB_ENABLE_PPROF` | `false` | NornicDB process | Enables NornicDB profiling. | Use after Eshu logs show the statement shape is correct but NornicDB runtime cost remains unknown. |
| `NORNICDB_ADDRESS`, `NORNICDB_BOLT_PORT`, `NORNICDB_HTTP_PORT`, `NORNICDB_DATA_DIR`, `NORNICDB_AUTH`, `NORNICDB_DEFAULT_DATABASE`, `NORNICDB_HEADLESS`, `NORNICDB_MCP_ENABLED` | local Eshu service sets these in process mode | NornicDB process | External NornicDB process configuration. | Internal for explicit process-mode runs; set manually only when running `nornicdb serve` outside Eshu. |

## Workflow Coordinator

| Variable | Default | Read By | Purpose | Tune When |
| --- | --- | --- | --- | --- |
| `ESHU_WORKFLOW_COORDINATOR_DEPLOYMENT_MODE` | `dark` | workflow coordinator | Coordinator mode: `dark` or `active`. | Set to `active` only when deploying coordinator-owned claims. |
| `ESHU_WORKFLOW_COORDINATOR_CLAIMS_ENABLED` | `false` | workflow coordinator | Enables workflow claims. | Enable only with coordinated collector instances. |
| `ESHU_WORKFLOW_COORDINATOR_ENABLE_CLAIMS` | `false` | workflow coordinator | Backward-compatible claims flag. | Prefer `ESHU_WORKFLOW_COORDINATOR_CLAIMS_ENABLED`. |
| `ESHU_WORKFLOW_COORDINATOR_RECONCILE_INTERVAL` | `30s` | workflow coordinator | Desired-state reconcile interval. | Lower for faster claim orchestration; raise to reduce DB work. |
| `ESHU_WORKFLOW_COORDINATOR_REAP_INTERVAL` | workflow default | workflow coordinator | Expired-claim reap interval. | Tune when failed collectors take too long or too little time to be reaped. |
| `ESHU_WORKFLOW_COORDINATOR_CLAIM_LEASE_TTL` | workflow default | workflow coordinator | Collector claim TTL. | Must be greater than heartbeat interval; raise for long collector work. |
| `ESHU_WORKFLOW_COORDINATOR_HEARTBEAT_INTERVAL` | workflow default | workflow coordinator | Collector claim heartbeat interval. | Keep below claim TTL; lower for faster liveness signal. |
| `ESHU_WORKFLOW_COORDINATOR_EXPIRED_CLAIM_LIMIT` | workflow default | workflow coordinator | Max expired claims reaped per cycle. | Raise only if expired-claim backlog grows. |
| `ESHU_WORKFLOW_COORDINATOR_EXPIRED_CLAIM_REQUEUE_DELAY` | workflow default | workflow coordinator | Delay before requeueing expired work. | Tune to avoid immediate flapping after collector loss. |
| `ESHU_COLLECTOR_INSTANCES_JSON` | unset | workflow coordinator, collector-terraform-state | Desired collector instance list. | Set from deployment config, not ad hoc shell sessions. Terraform-state runtimes select their enabled claim-capable instance from this list. |

Active coordinator mode is intentionally guarded. The process rejects
`ESHU_WORKFLOW_COORDINATOR_DEPLOYMENT_MODE=active` unless
`ESHU_WORKFLOW_COORDINATOR_CLAIMS_ENABLED=true` and at least one enabled
collector instance has `claims_enabled: true`. The Helm chart still permits
dark mode only; use the Compose profile for active proof runs until the remote
full-corpus validation is complete.

## Terraform State Collector

| Variable | Default | Read By | Purpose | Tune When |
| --- | --- | --- | --- | --- |
| `ESHU_TFSTATE_REDACTION_KEY` | unset | collector-terraform-state | Deployment-scoped secret material for deterministic redaction markers. | Required before the runtime can parse Terraform state. Rotate as a secret; do not use it as a tuning knob. |
| `ESHU_TFSTATE_REDACTION_RULESET_VERSION` | unset | collector-terraform-state | Version string for the redaction rule set the collector applies to every parsed attribute. | Required at startup. Blank values make `redact.RuleSet` fail closed (every scalar redacted, every composite dropped), silently breaking attribute-level drift detection. Bump when the schema-trust rule set changes so audit evidence ties each decision to a policy version. |
| `ESHU_TFSTATE_REDACTION_SENSITIVE_KEYS` | `password,secret,token,access_key,private_key,certificate,key_pair` | collector-terraform-state | Comma-separated leaf attribute keys treated as redacted secrets. | Extend per provider schema. Empty entries are ignored. |
| `ESHU_TFSTATE_COLLECTOR_INSTANCE_ID` | required when more than one enabled Terraform-state instance exists | collector-terraform-state | Selects the claim-capable `terraform_state` instance from `ESHU_COLLECTOR_INSTANCES_JSON`. | Set whenever multiple Terraform-state collectors are deployed. |
| `ESHU_TFSTATE_COLLECTOR_OWNER_ID` | host/process-derived | collector-terraform-state | Owner label written into workflow claim rows. | Set to a stable pod or worker name when debugging claim ownership. |
| `ESHU_TFSTATE_COLLECTOR_POLL_INTERVAL` | `1s` | collector-terraform-state | Delay between empty claim polls. | Raise to reduce idle DB polling; lower only when claim pickup latency matters. |
| `ESHU_TFSTATE_COLLECTOR_CLAIM_LEASE_TTL` | workflow default | collector-terraform-state | Lease TTL used when claiming and refreshing work. | Raise only if valid state reads or commits approach the current lease. |
| `ESHU_TFSTATE_COLLECTOR_HEARTBEAT_INTERVAL` | workflow default | collector-terraform-state | Heartbeat interval for active workflow claims. | Keep below claim TTL. Lower for faster stale-worker detection. |
| `ESHU_TFSTATE_SOURCE_MAX_BYTES` | reader default | collector-terraform-state | Max bytes read from one local or S3 state source. | Lower to fail fast on unexpectedly large state; raise only after confirming the state object is legitimate. |

## Telemetry, Memory, And Compose

| Variable | Default | Read By | Purpose | Tune When |
| --- | --- | --- | --- | --- |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | unset | telemetry bootstrap | Enables OTLP traces and metrics. | Set in deployments or the Compose telemetry overlay when exporting to collector/Jaeger. |
| `OTEL_EXPORTER_OTLP_PROTOCOL` | deployment/overlay set | OTEL SDK | OTLP transport protocol, normally `grpc` in the local telemetry overlay. | Set with the collector endpoint; do not use as a Eshu performance knob. |
| `OTEL_EXPORTER_OTLP_INSECURE` | deployment/overlay set | OTEL SDK | Allows insecure OTLP transport for local Compose telemetry. | Local/dev collector setups only. |
| `OTEL_TRACES_EXPORTER` | deployment/overlay set | OTEL SDK | Selects trace exporter, normally `otlp`. | Deployment telemetry wiring only. |
| `OTEL_METRICS_EXPORTER` | deployment/overlay set | OTEL SDK | Selects metrics exporter, normally `otlp`. | Deployment telemetry wiring only. |
| `OTEL_LOGS_EXPORTER` | `none` in telemetry overlay | OTEL SDK | Disables OTEL log export while Eshu emits JSON stderr logs. | Leave as `none` unless the logging pipeline explicitly supports OTEL logs. |
| `OTEL_SERVICE_NAME` | binary/service name | OTEL SDK | Overrides service name resource attribute. | Usually set by Helm/deployment templates, not manually. |
| `GOMEMLIMIT` | Go runtime default or cgroup-derived 70% when configured by Eshu | Go runtime | Soft heap target. | Set when cgroup detection is unavailable or container memory needs a deliberate heap budget. |
| `GODEBUG` | unset | Go runtime / Eshu memlimit setup | Go runtime debug flags; Eshu may preserve/add memory-limit related settings. | Use only for Go runtime diagnostics. |
| `ESHU_DEPLOYMENT_ENVIRONMENT` | `local-compose` in compose | deployment/compose | Environment label injected into runtime env. | Set from deployment metadata so logs/traces identify environment. |
| `ESHU_PROMETHEUS_METRICS_ENABLED` | `true` in compose services | deployment/compose | Enables compose/service Prometheus scrape path. | Deployment wiring only. |
| `ESHU_PROMETHEUS_METRICS_PORT` | `9464` in compose services | deployment/compose | In-container metrics port used by services. | Change only with matching service port wiring. |
| `ESHU_FILESYSTEM_HOST_ROOT` | `./tests/fixtures/ecosystems` in compose | Docker Compose | Host repo root mounted into compose fixtures path. | Set to an absolute real directory for compose validation against local repos. |
| `ESHU_ESHUIGNORE_PATH` | `/dev/null` in compose | Docker Compose | Optional host `.eshuignore` mounted into compose bootstrap/ingester containers. | Set when compose validation should use a specific ignore file. |
| `ESHU_HTTP_PORT` | `8080` in compose | Docker Compose | Host port for Eshu API. | Change to avoid local port conflicts. |
| `ESHU_MCP_PORT` | `8081` in compose | Docker Compose | Host port for MCP HTTP service. | Change to avoid local port conflicts. |
| `ESHU_API_METRICS_PORT` | `19464` in compose | Docker Compose | Host metrics port for API. | Change to avoid local port conflicts. |
| `ESHU_INGESTER_METRICS_PORT` | `19465` in compose | Docker Compose | Host metrics port for ingester. | Change to avoid local port conflicts. |
| `ESHU_RESOLUTION_ENGINE_METRICS_PORT` | `19466` in compose | Docker Compose | Host metrics port for resolution engine. | Change to avoid local port conflicts. |
| `ESHU_BOOTSTRAP_METRICS_PORT` | `19467` in compose | Docker Compose | Host metrics port for bootstrap-index job/service. | Change to avoid local port conflicts. |
| `ESHU_MCP_METRICS_PORT` | `19468` in compose | Docker Compose | Host metrics port for MCP server. | Change to avoid local port conflicts. |
| `ESHU_WORKFLOW_COORDINATOR_HTTP_PORT` | `18082` in compose | Docker Compose | Host HTTP port for workflow coordinator. | Change to avoid local port conflicts. |
| `ESHU_WORKFLOW_COORDINATOR_METRICS_PORT` | `19469` in compose | Docker Compose | Host metrics port for workflow coordinator. | Change to avoid local port conflicts. |
| `NEO4J_HTTP_PORT` | `7474` in compose examples | Docker Compose | Host Neo4j HTTP port. | Change to avoid local port conflicts. |
| `NEO4J_BOLT_PORT` | `7687` in compose examples | Docker Compose | Host Neo4j Bolt port. | Change to avoid local port conflicts. |
| `NEO4J_AUTH` | `neo4j/${ESHU_NEO4J_PASSWORD:-change-me}` in compose | Neo4j container | Neo4j container auth string. | Prefer setting `ESHU_NEO4J_PASSWORD` rather than editing this directly. |
| `NEO4J_AUTH_ENABLED` | `true` in legacy compose template | Neo4j container | Enables Neo4j auth in the template variant. | Leave enabled except for disposable local debugging. |
| `NEO4J_PLUGINS` | `[]` in compose | Neo4j container | Neo4j plugin list. | Leave empty unless a documented graph feature requires a plugin. |
| `ESHU_NEO4J_HEAP_INITIAL_SIZE` | `512m` in compose | Docker Compose Neo4j container | Neo4j initial heap size. | Raise when compose Neo4j OOMs or GC thrashes during validation. |
| `ESHU_NEO4J_HEAP_MAX_SIZE` | `512m` in compose | Docker Compose Neo4j container | Neo4j max heap size. | Raise with heap evidence; keep within host memory. |
| `ESHU_NEO4J_PAGECACHE_SIZE` | `512m` in docs/examples | Docker Compose Neo4j container | Neo4j page cache budget. | Raise when graph read/write workloads are page-cache bound and host memory allows. |
| `ESHU_POSTGRES_PORT` | `15432` in compose | Docker Compose | Host Postgres port. | Change to avoid local port conflicts or expose compose Postgres to host-side tests. |
| `ESHU_POSTGRES_PASSWORD` | `change-me` in compose | Docker Compose/Postgres | Password used by compose Postgres and generated DSNs. | Change for non-local compose; update matching DSNs/secrets. |
| `ESHU_PG_SHARED_BUFFERS` | `4GB` in compose | Postgres container | Postgres shared buffers. | Tune only with Postgres memory/IO evidence. |
| `ESHU_PG_WORK_MEM` | `16MB` in compose | Postgres container | Per-operation work memory. | Raise carefully for sort/hash-heavy queries; multiplied by concurrency. |
| `ESHU_PG_MAINTENANCE_WORK_MEM` | `512MB` in compose | Postgres container | Maintenance operation memory. | Raise for index/build maintenance if host memory allows. |
| `ESHU_PG_MAX_WAL_SIZE` | `8GB` in compose | Postgres container | WAL size before checkpoint pressure. | Raise if checkpoint churn appears during large ingest. |
| `ESHU_PG_WAL_BUFFERS` | `64MB` in compose | Postgres container | WAL buffer budget. | Tune only with Postgres WAL evidence. |
| `ESHU_PG_EFFECTIVE_CACHE_SIZE` | `32GB` in compose | Postgres container | Planner estimate of OS cache. | Match host/container memory; not a direct allocation. |
| `ESHU_PG_SYNCHRONOUS_COMMIT` | `off` in compose | Postgres container | Commit durability/latency trade-off for local compose. | Keep production durability policy explicit; compose uses speed-oriented local default. |
| `ESHU_PG_TOAST_COMPRESSION` | `lz4` in compose | Postgres container | TOAST compression algorithm. | Change only for Postgres compatibility or storage experiments. |
| `OTEL_COLLECTOR_OTLP_GRPC_PORT` | `4317` in compose | Docker Compose | Host OTLP gRPC port. | Change to avoid local port conflicts. |
| `OTEL_COLLECTOR_OTLP_HTTP_PORT` | `4318` in compose | Docker Compose | Host OTLP HTTP port. | Change to avoid local port conflicts. |
| `OTEL_COLLECTOR_PROMETHEUS_PORT` | `9464` in compose | Docker Compose | Host Prometheus scrape/export port. | Change to avoid local port conflicts. |

## Terraform Schema

| Variable | Default | Read By | Purpose | Tune When |
| --- | --- | --- | --- | --- |
| `ESHU_TERRAFORM_SCHEMA_DIR` | packaged/default schema dir | Terraform schema loader | Overrides Terraform provider schema directory. | Use for local schema development or testing newly generated provider schemas. |

## Test And Perf Gates

| Variable | Default | Read By | Purpose | Tune When |
| --- | --- | --- | --- | --- |
| `ESHU_LOCAL_AUTHORITATIVE_PERF` | unset / `false` | opt-in Go tests | Enables local-authoritative startup/query performance smoke tests. | Set only for local-authoritative smoke tests. Use `-tags nolocalllm` for embedded mode, or set `ESHU_NORNICDB_RUNTIME=process` plus `ESHU_NORNICDB_BINARY` for process-mode tests. |

## Deprecated Or Unsupported

These names may appear in older docs, scripts, or historical configs. They are
not supported tuning surfaces for the current Go runtime:

- `ESHU_REPO_FILE_PARSE_MULTIPROCESS`
- `ESHU_MULTIPROCESS_START_METHOD`
- `ESHU_WORKER_MAX_TASKS`
- `ESHU_INDEX_QUEUE_DEPTH`
- `ESHU_WATCH_DEBOUNCE_SECONDS`
- `ESHU_COMMIT_WORKERS`
- `ESHU_MAX_CALLS_PER_FILE`

If one of these looks necessary, stop and identify which current Go stage owns
the behavior before adding a replacement knob.
