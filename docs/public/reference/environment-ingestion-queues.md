# Ingestion And Queue Environment

This page covers repository discovery, parsing, projector, reducer, queue,
graph-write, and NornicDB tuning variables. Change these only with throughput,
queue, or graph-write evidence.

## Repository Discovery And Parsing

| Variable | Default | Read by | Purpose |
| --- | --- | --- | --- |
| `ESHU_REPO_SOURCE_MODE` | `githubOrg` | collector, ingester, bootstrap-index | Repository source mode: `githubOrg`, `explicit`, or `filesystem`. |
| `ESHU_GITHUB_ORG` | unset | GitHub selector | GitHub organization to discover. |
| `ESHU_REPOSITORY_RULES_JSON` | unset | collector selector | Exact/regex include rules; exact rules define repos for `explicit` and `filesystem`. |
| `ESHU_REPOS_DIR` | `/data/repos` | collector | Local clone/cache directory. |
| `ESHU_REPO_LIMIT` | `4000` | GitHub selector | Maximum repos discovered in one cycle. |
| `ESHU_REPO_SHARD_COUNT` | `1` | collector selector | Deterministic repository shard count. Values greater than `1` filter discovered repository IDs before filesystem or Git sync. |
| `ESHU_REPO_SHARD_INDEX` | `0` | collector selector | Zero-based shard index; must be less than `ESHU_REPO_SHARD_COUNT`. Helm sets shard count from `ingester.replicas` and shard index from the StatefulSet pod ordinal when horizontal ingesters are enabled. |
| `ESHU_CLONE_DEPTH` | `1` | collector | Git clone depth. |
| `ESHU_GIT_AUTH_METHOD` | `githubApp` | collector | Git auth mode. |
| `ESHU_GIT_TOKEN`, `GITHUB_TOKEN` | unset | collector | Token auth credential. |
| `ESHU_GITHUB_APP_ID`, `GITHUB_APP_ID` | unset | collector | GitHub App ID. |
| `ESHU_GITHUB_APP_INSTALLATION_ID`, `GITHUB_APP_INSTALLATION_ID` | unset | collector | GitHub App installation ID. |
| `ESHU_GITHUB_APP_PRIVATE_KEY`, `GITHUB_APP_PRIVATE_KEY` | unset | collector | GitHub App private key. |
| `ESHU_SSH_PRIVATE_KEY_PATH` | unset | collector | SSH key path for SSH clone auth. |
| `ESHU_SSH_KNOWN_HOSTS_PATH` | unset | collector | Known-hosts path for SSH clone verification. |
| `ESHU_INCLUDE_ARCHIVED_REPOS` | `false` | GitHub selector | Include archived repos. |
| `ESHU_FILESYSTEM_ROOT` | unset | filesystem selector | Root path for filesystem source mode. |
| `ESHU_FILESYSTEM_DIRECT` | `false` | filesystem selector | Treat filesystem root as direct source rather than cloned cache flow. |
| `ESHU_SNAPSHOT_WORKERS` | `min(NumCPU, 8)`; local-authoritative owner uses `NumCPU` | collector | Concurrent repo snapshot workers. |
| `ESHU_PARSE_WORKERS` | `min(NumCPU, 8)`; local-authoritative owner uses `NumCPU` | collector snapshotter | Concurrent file parse workers inside each snapshot. |
| `ESHU_EMIT_DATAFLOW` | `false` | collector snapshotter | Opt-in value-flow emission. When enabled (`1`, `true`, `yes`, `on`) the per-file parser emits the `dataflow_functions`, `taint_findings`, and `interproc_findings` buckets (plus durable `dataflow_summaries` and `dataflow_sources` when a repository id and package import path are present). Off by default; the snapshot payload is byte-identical when off. See [Value-Flow Emission](value-flow-emission.md). |
| `ESHU_STREAM_BUFFER` | `0` | collector | Generation stream buffer; `0` derives from worker count. |
| `ESHU_BOOTSTRAP_COMMIT_LANES` | `4` | bootstrap-index | Concurrent commit lanes for the bootstrap collector drain (#5130). The effective count is clamped to the measured 4-lane plateau from the #5122 lane shim (not CPU count) AND to `ESHU_POSTGRES_MAX_OPEN_CONNS` headroom after reserving `max(2, projection_workers + 1)` connections for the concurrent projector — every lane holds an open transaction. Invalid or non-positive values fall back to the default; the startup log reports the effective `commit_lanes`. |
| `ESHU_LARGE_REPO_FILE_THRESHOLD` | `1000` | collector | File-count threshold for large-repo semaphore. |
| `ESHU_LARGE_REPO_MAX_CONCURRENT` | `2` | collector | Concurrent large repo snapshots. |
| `ESHU_REPO_RECONCILE_INTERVAL_HOURS` | `24` | collector | Hours a git scope may go without a full observation before the sweep forces one to retract delta drift; `0` disables reconciliation. |
| `ESHU_REPO_RECONCILE_MAX_PER_CYCLE` | `10` | collector | Max scopes forced to a full reconciliation snapshot per selection cycle; `0` removes the per-cycle cap. |
| `ESHU_PINNED_REFS_JSON` | unset | collector (githubOrg, explicit modes) | Per-repository pinned ref map. JSON object mapping repository IDs (config-form after `normalizeRepositoryID`, e.g. `"org/repo"`, NOT `repository:r_<hash>`) to an array of ref names (branches or tags, e.g. `["feature-x", "v1.0"]`). Empty means feature-off. Both branches (heads-first lookup) and tags (fallback) are supported. Filesystem mode is inert (the feature only operates on git-synced repositories). Enabler for epic #5393, issue #5417. |
| `ESHU_PINNED_REF_PER_REPO_CAP` | `3` | collector | Maximum pinned refs per repository. Counts exceeding the cap are truncated with a warning log at sync time (not rejected at parse time). |
| `ESHU_PINNED_REF_FLEET_CAP` | `0` (unlimited) | collector | Absolute maximum total pinned-ref worktree entries per sync cycle across the entire fleet. Zero means unlimited. Set after measuring the true per-ref cost on your corpus (deferred to #5393 Phase 0). |
| `ESHU_DISCOVERY_REPORT` | unset | `eshu index`, bootstrap-index | Writes per-repo discovery advisory JSON. |
| `ESHU_DISCOVERY_IGNORED_PATH_GLOBS` | unset | bootstrap-index, collector-git, ingester | Operator ignore overlay. Entries may use `pattern=reason`. |
| `ESHU_DISCOVERY_PRESERVED_PATH_GLOBS` | unset | bootstrap-index, collector-git, ingester | Preserved globs that override broader ignored ancestors. |
| `ESHU_BOOTSTRAP_IS_DEPENDENCY` | `false` | collector | Marks bootstrap source as dependency package. |
| `ESHU_BOOTSTRAP_PACKAGE_NAME` | unset | collector | Dependency package name. |
| `ESHU_BOOTSTRAP_PACKAGE_LANGUAGE` | unset | collector | Dependency package language. |
| `SCIP_INDEXER` | `false` | collector snapshotter | Enables SCIP supplement indexing only when set to `1`, `true`, `yes`, or `on` and selected language package or workspace roots have matching `scip-*` binaries available. Unset, unrecognized, `false`, `0`, `no`, and `off` values keep native-only parsing. Outcome volume is visible through `eshu_dp_scip_snapshot_attempts_total{language,result}`. |
| `SCIP_LANGUAGES` | `python,typescript,javascript,go,rust,java,cpp,c` | collector snapshotter | Comma-separated SCIP language allowlist. Narrow this list to keep native parsing complete while limiting which language package or workspace roots can run SCIP. |
| `SCIP_WORKERS` | `4` | collector snapshotter | Bounded concurrent SCIP language/subtree indexer processes across the ingester snapshotter, including concurrent repository snapshots. Set to `1` for memory-constrained serial fallback; keep higher values aligned with host CPU and memory because each slot may run a compiler-grade indexer. |

Performance Evidence: issue #4455 aligned Docker Compose and Neo4j Compose with
the documented Go default `ESHU_LARGE_REPO_MAX_CONCURRENT=2`. Before the fix,
those Compose profiles injected `1`, so an unset value serialized large
repository snapshots even though the two-lane scheduler was designed to run two
large snapshots while small repositories continue on the remaining workers. A
bounded current-main diagnostic on the 895-root corpus used
`ESHU_SNAPSHOT_WORKERS=16`, `ESHU_PARSE_WORKERS=16`, and
`ESHU_LARGE_REPO_MAX_CONCURRENT=4`; after about 5m39s it had completed 136
repository snapshots and showed parser/pre-scan work still active while
source-local canonical graph writes became the next bottleneck. This change is a
scheduling-default correction, not terminal full-corpus wall-clock proof; #4455
tracks the required bounded `1` versus `2`/`4` comparison.

No-Observability-Change: the change adds no metric, span, log key, queue,
worker type, or graph-write path. Operators continue to use
`eshu_dp_large_repo_classifications_total`,
`eshu_dp_large_repo_semaphore_wait_seconds`, and the structured
`large repository queued`, `large repo semaphore acquired`, and
`large repo semaphore released` logs to confirm classification, wait time, and
held time for large repositories.

## Incremental Refresh

| Variable | Default | Read by | Purpose |
| --- | --- | --- | --- |
| `ESHU_WEBHOOK_TRIGGER_HANDOFF_ENABLED` | `false` | ingester webhook-trigger selector | Checks queued webhook refresh triggers before scheduled repository polling. |
| `ESHU_REPO_SCHEDULED_SYNC_ENABLED` | `true` | ingester | Enables broad scheduled repository selection when no webhook triggers are queued. Startup rejects `false` unless webhook handoff is enabled. |
| `ESHU_WEBHOOK_TRIGGER_HANDOFF_OWNER` | `ingester` | ingester webhook-trigger selector | Lease owner recorded when claiming queued Git webhook refresh triggers. |
| `ESHU_WEBHOOK_TRIGGER_CLAIM_LIMIT` | `100` | ingester webhook-trigger selector | Maximum webhook refresh triggers claimed in one selector pass. |

## Projection And Reducer Queues

| Variable | Default | Read by | Purpose |
| --- | --- | --- | --- |
| `ESHU_PROJECTOR_WORKERS` | `min(NumCPU, 8)`; NornicDB local-authoritative uses `NumCPU` | ingester projector | Source-local projector worker count. |
| `ESHU_REDUCER_ADMISSION_HIGH_WATER_MARK` | `10000`; set `0` to disable | ingester projector | Defers source-local reducer intent enqueues while outstanding reducer queue depth is at or above this threshold. |
| `ESHU_REDUCER_ADMISSION_POLL_INTERVAL` | `1s` | ingester projector | Queue-depth recheck interval while reducer admission is deferring. Must be greater than zero when set. |
| `ESHU_LARGE_GEN_THRESHOLD` | `10000` facts | ingester projector | Fact-count threshold for large-generation semaphore. |
| `ESHU_LARGE_GEN_MAX_CONCURRENT` | default `2`; local-authoritative `4` | ingester projector | Concurrent large source-local generations. |
| `ESHU_PROJECTOR_MAX_ATTEMPTS` | `3` | ingester/projector retry policy | Max projector attempts before terminal failure. |
| `ESHU_PROJECTOR_RETRY_DELAY` | `30s` | ingester/projector retry policy | Delay between projector retries. |
| `ESHU_PROJECTOR_RETRY_ONCE_SCOPE_GENERATION` | unset | projector runtime | Test/fault-injection retry hook for one scope generation. |
| `ESHU_PROJECTION_WORKERS` | `min(NumCPU, 8)` | bootstrap-index | Bootstrap projection worker count. |
| `ESHU_DEFERRED_BACKFILL_CONCURRENCY` | `min(NumCPU, 8)`, hard cap `8` | bootstrap-index / ingester deferred maintenance | Concurrent per-repository batch transactions in the deferred relationship-evidence backfill. Each batch holds one pooled connection for its transaction and never nests a second, so a value above the connection pool throttles on `Begin` rather than deadlocking. Set to `1` when `ESHU_POSTGRES_MAX_OPEN_CONNS=1` (single-connection pool) so the pass runs serially; the default uses the existing cap on roomy hosts to shorten the backfill long pole. |
| `ESHU_REDUCER_WORKERS` | Neo4j: `min(NumCPU, 4)`; NornicDB: `NumCPU` | reducer | Reducer intent worker count. |
| `ESHU_REDUCER_BATCH_CLAIM_SIZE` | Neo4j: `workers*4` capped `4..64`; NornicDB: `workers` | reducer | Number of reducer intents claimed per poll. |
| `ESHU_REDUCER_EXPECTED_SOURCE_LOCAL_PROJECTORS` | unset; local owner sets discovered repo count | reducer | Gates NornicDB/local-authoritative semantic entity claims until source-local projectors drain. |
| `ESHU_REDUCER_SEMANTIC_ENTITY_CLAIM_LIMIT` | unset / disabled | reducer | Optional cap on cross-scope semantic entity materialization claims after source-local drain. |
| `ESHU_REDUCER_CLAIM_DOMAIN` | unset | reducer | Restricts main reducer claim loop to one domain. Cannot combine with `ESHU_REDUCER_CLAIM_DOMAINS`. |
| `ESHU_REDUCER_CLAIM_DOMAINS` | unset | reducer | Comma-separated reducer domain allowlist for domain-specific lanes. |
| `ESHU_DRIFT_PRIOR_CONFIG_DEPTH` | `10` | reducer drift loader | Prior repo-snapshot generations walked for Terraform resources removed from config. |
| `ESHU_REDUCER_MAX_ATTEMPTS` | `3` | reducer retry policy | Max reducer attempts before terminal failure. |
| `ESHU_REDUCER_RETRY_DELAY` | `30s` | reducer retry policy | Delay between reducer retries. |
| `ESHU_REDUCER_SECRETS_IAM_GRAPH_PROJECTION_ENABLED` | `false` | reducer | Opt-in for live secrets/IAM trust-chain graph projection (ADR #1314 §4). Unset/`false` keeps `DomainSecretsIAMGraphProjection` unregistered and writes nothing; a malformed value is a startup error. Leave unset until the target-bound activation record in #2430 names the deployment, binds security/schema approval, and records flag-on proof. |

## Shared Projection And Repair

| Variable | Default | Read by | Purpose |
| --- | --- | --- | --- |
| `ESHU_SHARED_PROJECTION_WORKERS` | `min(NumCPU, 4)`; Compose defaults to `4`; Helm sets `8` | reducer shared projection | Partition worker count for shared projection domains. |
| `ESHU_SHARED_PROJECTION_PARTITION_COUNT` | `8` | reducer shared projection | Partitions per shared domain. |
| `ESHU_SHARED_PROJECTION_BATCH_LIMIT` | `100` | reducer shared projection | Intents per partition batch. |
| `ESHU_SHARED_PROJECTION_POLL_INTERVAL` | `500ms` | reducer shared projection | Idle poll interval; idle cycles back off up to `5s`. |
| `ESHU_SHARED_PROJECTION_LEASE_TTL` | `60s` | reducer shared projection | Partition lease TTL. |
| `ESHU_CODE_CALL_PROJECTION_POLL_INTERVAL` | `500ms` | reducer code-call sidecar | Idle poll interval for code-call projection. |
| `ESHU_CODE_CALL_PROJECTION_LEASE_TTL` | `60s` | reducer code-call sidecar | Lease TTL for code-call projection work. |
| `ESHU_CODE_CALL_PROJECTION_BATCH_LIMIT` | `100` | reducer code-call sidecar | Claim batch size for code-call work. |
| `ESHU_CODE_CALL_PROJECTION_ACCEPTANCE_SCAN_LIMIT` | `250000` | reducer code-call sidecar | Guard for complete accepted repo/run scan before rewriting `CALLS` edges. |
| `ESHU_CODE_CALL_PROJECTION_LEASE_OWNER` | `code-call-projection-runner` | reducer code-call sidecar | Lease owner name. |
| `ESHU_CODE_CALL_PROJECTION_PARTITION_COUNT` | `8` | reducer code-call sidecar | Partition count for file-scoped CALLS projection lanes. |
| `ESHU_CODE_CALL_PROJECTION_WORKERS` | `4` | reducer code-call sidecar | Concurrent partition workers for file-scoped CALLS projection lanes. |
| `ESHU_REPO_DEPENDENCY_PROJECTION_POLL_INTERVAL` | `500ms` | reducer repo-dependency sidecar | Idle poll interval. |
| `ESHU_REPO_DEPENDENCY_PROJECTION_LEASE_TTL` | `5m` | reducer repo-dependency sidecar | Shard lease and fail-closed quarantine window. Must exceed the cycle timeout plus `ESHU_CANONICAL_WRITE_TIMEOUT` and `30s`. |
| `ESHU_REPO_DEPENDENCY_PROJECTION_CYCLE_TIMEOUT` | `45s` | reducer repo-dependency sidecar | Deadline for selection, repository lock, lease validation, graph replacement, completion, and Postgres commit after the shard is claimed. |
| `ESHU_REPO_DEPENDENCY_PROJECTION_BATCH_LIMIT` | `100` | reducer repo-dependency sidecar | Claim batch size. |
| `ESHU_REPO_DEPENDENCY_PROJECTION_LEASE_OWNER` | `repo-dependency-projection-runner` | reducer repo-dependency sidecar | Owner prefix; the reducer appends hostname, PID, and a boot-unique nonce. |
| `ESHU_REPO_DEPENDENCY_PROJECTION_WORKERS` | `4` on NornicDB; `1` on Neo4j | reducer repo-dependency sidecar | Fixed acceptance-unit shard count. Allowed values are `1`, `2`, and `4`; unsupported values fall back to the backend default. |
| `ESHU_REPO_DEPENDENCY_RETRACT_STATEMENT_TIMING` | `false` | reducer repo-dependency sidecar | Compatibility variable; behavior is always sequential auto-commit retracts with per-role timing. Grouped DELETEs under-apply on pinned NornicDB. |
| `ESHU_GRAPH_PROJECTION_REPAIR_POLL_INTERVAL` | `1s` | reducer repairer | Poll interval for graph projection phase repair. |
| `ESHU_GRAPH_PROJECTION_REPAIR_BATCH_LIMIT` | `100` | reducer repairer | Repair rows per batch. |
| `ESHU_GRAPH_PROJECTION_REPAIR_RETRY_DELAY` | `1m` | reducer repairer | Delay before retrying repair. |
| `ESHU_CODE_VALUE_FLOW_STALE_CLEANUP_ENABLED` | `true` | reducer value-flow cleanup | Enables bounded stale cleanup for reducer-owned value-flow graph evidence. |
| `ESHU_CODE_VALUE_FLOW_STALE_CLEANUP_POLL_INTERVAL` | `1h` | reducer value-flow cleanup | Idle poll interval after an exhausted or failed cleanup pass. |
| `ESHU_CODE_VALUE_FLOW_STALE_CLEANUP_LEASE_OWNER` | unique per process | reducer value-flow cleanup | Lease owner for the single cleanup worker. |
| `ESHU_CODE_VALUE_FLOW_STALE_CLEANUP_LEASE_TTL` | `5m` | reducer value-flow cleanup | TTL for the cleanup lease. |
| `ESHU_CODE_VALUE_FLOW_STALE_CLEANUP_SCOPE_BATCH_LIMIT` | `100` | reducer value-flow cleanup | Active repository scopes scanned per pass. |
| `ESHU_CODE_VALUE_FLOW_STALE_CLEANUP_DELETE_BATCH_LIMIT` | `500` | reducer value-flow cleanup | Stale evidence nodes or edges deleted per scope and family in one Cypher statement. |

Performance Evidence: the #2624 baseline remote proof rendered file-scoped
`code_calls` work but leased the domain with `partition_count=1`, while the
queue held 3,454 distinct code-call partition keys and 18,857 pending
file-scoped intents. After this configuration change, the rendered remote E2E
profile reports `ESHU_CODE_CALL_PROJECTION_PARTITION_COUNT=4` and
`ESHU_CODE_CALL_PROJECTION_WORKERS=2` for runtime services on the pinned
NornicDB backend. The terminal #2599 remote proof must confirm `code_calls`
`partition_count > 1`, zero dead letters, and drained or bounded queue state.

Performance Evidence: issue #2995 raises the reducer runtime defaults to
`ESHU_CODE_CALL_PROJECTION_PARTITION_COUNT=8` and
`ESHU_CODE_CALL_PROJECTION_WORKERS=4`, and Helm now renders those defaults plus
`ESHU_SHARED_PROJECTION_PARTITION_COUNT=8` for hosted resolution-engine pods.
The storage lease claim now serializes same-domain claims with a
transaction-scoped advisory lock and refuses a new `partition_count` while any
active same-domain lease for another count remains unexpired, so rolling count
changes fail closed instead of racing overlapping file sets. Covered locally by
`go test ./cmd/reducer -run TestLoadCodeCallProjectionConfigDefaultsAcceptanceScanLimit -count=1`,
`go test ./internal/runtime -run TestHelmResolutionEngineRendersCodeCallProjectionConcurrency -count=1`,
and `go test ./internal/storage/postgres -run TestSharedIntentStoreClaimPartitionLeaseBlocksActivePartitionCountRescale -count=1`.

No-Observability-Change: these changes only adjust reducer sidecar defaults,
Helm-rendered environment, and partition lease admission. They add no metric,
span, status field, route, graph query, queue table, Cypher, or graph-write
shape. Operators still diagnose partition count, active claims, retries,
dead letters, and throughput through existing shared-projection lease rows,
`/admin/status` domain backlog, reducer code-call cycle logs,
`eshu_dp_queue_claim_duration_seconds{queue="code_calls"}`, graph write
metrics, and pprof surfaces.

## Graph Write Shape And NornicDB

For decision rules and evidence requirements, read [NornicDB Tuning](nornicdb-tuning.md).

| Variable | Default | Read by | Purpose |
| --- | --- | --- | --- |
| `ESHU_CANONICAL_WRITE_TIMEOUT` | `30s` on NornicDB | bootstrap-index, ingester, projector, reducer graph writers | Client context and Bolt transaction timeout for NornicDB writes. |
| `ESHU_NORNICDB_PHASE_GROUP_STATEMENTS` | `500` | graph writer | Broad grouped statement cap for phases without a narrower cap. |
| `ESHU_NORNICDB_FILE_PHASE_GROUP_STATEMENTS` | `5` | graph writer | Grouped statement cap for `phase=files`. |
| `ESHU_NORNICDB_FILE_BATCH_SIZE` | `100` | graph writer | Rows per file-upsert statement. |
| `ESHU_NORNICDB_ENTITY_PHASE_GROUP_STATEMENTS` | `25` | graph writer | Grouped statement cap for canonical entity phases. |
| `ESHU_NORNICDB_ENTITY_BATCH_SIZE` | `100` | graph writer | Default rows per canonical entity statement. |
| `ESHU_NORNICDB_ENTITY_LABEL_BATCH_SIZES` | `Function=15,K8sResource=1,Struct=50,Variable=100` | graph writer | Label-specific canonical entity row caps. |
| `ESHU_NORNICDB_ENTITY_LABEL_PHASE_GROUP_STATEMENTS` | `Function=5,K8sResource=1,Struct=15,Variable=5` | graph writer | Label-specific grouped statement caps. |
| `ESHU_NORNICDB_ENTITY_PHASE_CONCURRENCY` | `NumCPU` clamped to `16` | bootstrap-index / ingester / projector graph writer | Worker count for parallel canonical entity-phase chunk dispatch. |
| `ESHU_NORNICDB_SEMANTIC_ENTITY_LABEL_BATCH_SIZES` | `Annotation=5,Function=10,ImplBlock=10,Module=10,TypeAlias=5,TypeAnnotation=50,Variable=10` | reducer semantic writer | Label-specific semantic entity row caps. |
| `ESHU_CODE_CALL_EDGE_BATCH_SIZE` | `1000` | reducer code-call edge writer | Rows per code-call edge write statement. |
| `ESHU_CODE_CALL_EDGE_GROUP_BATCH_SIZE` | `1` | reducer code-call edge writer | Statements per grouped code-call edge execution. |
| `ESHU_INHERITANCE_EDGE_GROUP_BATCH_SIZE` | `1` | reducer shared edge writer | Grouped statements for inheritance edges. |
| `ESHU_SQL_RELATIONSHIP_EDGE_GROUP_BATCH_SIZE` | `1` | reducer shared edge writer | Statements per grouped SQL relationship write on Neo4j. NornicDB uses one-statement auto-commit because its managed transaction can acknowledge without persisting this MERGE shape (#5410). |
| `ESHU_NORNICDB_CANONICAL_GROUPED_WRITES` | `false` | graph writer | Conformance switch. On NornicDB, honored as per-dependency-phase commits, not a single grouped transaction (whole-materialization atomic is unsupported and would drop nested files, #4027). |
| `ESHU_NORNICDB_REQUIRE_GROUPED_ROLLBACK` | `false` | NornicDB tests | Makes grouped rollback conformance mandatory. |
| `ESHU_NORNICDB_BATCHED_ENTITY_CONTAINMENT` | unset / `true` | graph writer | Cross-file batched entity containment for NornicDB canonical entity writes. |
| `ESHU_NORNICDB_RUNTIME` | `embedded` | local Eshu service | Local NornicDB runtime: `embedded` or `process`. |
| `ESHU_NORNICDB_BINARY` | unset | local Eshu service, install, tests | Explicit process-mode NornicDB binary path. |
| `ESHU_NORNICDB_INSTALL_TIMEOUT` | `30s` | `eshu install nornicdb` | Download timeout for installer sources. |

## NornicDB Process And Container Variables

| Variable | Default | Read by | Purpose |
| --- | --- | --- | --- |
| `NORNICDB_ENABLE_PPROF` | `false` | NornicDB process | Enables NornicDB profiling. |
| `NORNICDB_ADDRESS`, `NORNICDB_BOLT_PORT`, `NORNICDB_HTTP_PORT`, `NORNICDB_DATA_DIR`, `NORNICDB_AUTH`, `NORNICDB_DEFAULT_DATABASE`, `NORNICDB_HEADLESS`, `NORNICDB_MCP_ENABLED` | local Eshu service sets these in process mode | NornicDB process | External NornicDB process configuration. |
| `NORNICDB_IMAGE` | `eshu-nornicdb-pr261:149245885258` in default Compose | Docker Compose | NornicDB image/tag override. Set with `NORNICDB_PULL_POLICY` for controlled comparisons. |
| `NORNICDB_PLATFORM` | unset | Docker Compose | Optional platform override; unset lets Docker choose host architecture. |
| `NORNICDB_PULL_POLICY` | `build` in default Compose; `missing` in tier-2 v25 proof | Docker Compose | Builds the exact pinned source by default. Use `always` with an immutable published-image override or `never` with a prebuilt local tag. |
| `NORNICDB_PERSIST_SEARCH_INDEXES` | `false` in Eshu Compose and Helm | NornicDB container | Keeps disabled BM25/vector search indexes from creating canonical graph restart artifacts. |
| `NORNICDB_SEARCH_BM25_ENABLED` | `false` in Eshu Compose and Helm | NornicDB container | Keeps BM25 indexing off for the canonical graph lane. |
| `NORNICDB_SEARCH_VECTOR_ENABLED` | `false` in Eshu Compose and Helm | NornicDB container | Keeps vector indexing off for the canonical graph lane. |
| `NORNICDB_SEARCH_BM25_WARMING` | `lazy` in Eshu Compose and Helm | NornicDB container | Uses lazy BM25 warming only when an operator deliberately enables BM25 for a search proof. |
| `NORNICDB_SEARCH_VECTOR_WARMING` | `lazy` in Eshu Compose and Helm | NornicDB container | Uses lazy vector warming only when an operator deliberately enables vector search for a search proof. |
| `NORNICDB_EMBEDDING_ENABLED` | `false` in Eshu Compose and Helm | NornicDB container | Keeps embedding workers off during Eshu indexing. |
| `NORNICDB_ASYNC_WRITES_ENABLED`, `NORNICDB_HEIMDALL_ENABLED`, `NORNICDB_QDRANT_GRPC_ENABLED` | `false` in Eshu Compose and Helm | NornicDB container | Keeps optional backend behavior off for the Eshu graph path. |

These controls are the supported graph-only policy for the canonical NornicDB
lane. Curated BM25, vector, or hybrid retrieval must use an explicit search
projection proof instead of indexing every canonical graph node and property by
default.
