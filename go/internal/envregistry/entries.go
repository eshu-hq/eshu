// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package envregistry

// Default returns the process-wide registry of supported ESHU_* environment
// variables: the core platform set plus the hosted-collector configuration set.
// It panics if the declarations are inconsistent (duplicate names or colliding
// aliases), because that is a programming error in these files rather than a
// runtime condition.
func Default() *Registry {
	all := make([]Entry, 0, len(coreEntries)+len(collectorEntries))
	all = append(all, coreEntries...)
	all = append(all, collectorEntries...)
	r, err := New(all)
	if err != nil {
		panic("envregistry: invalid Default registry: " + err.Error())
	}
	return r
}

// coreEntries declares the core-platform variables. Collector and
// registry-credential variables are intentionally out of scope; see the package
// doc. Keep entries grouped by subsystem and sorted within a group.
var coreEntries = []Entry{
	// postgres
	{Name: "ESHU_POSTGRES_DSN", Type: VarDSN, Subsystem: "postgres", Aliases: []string{"ESHU_FACT_STORE_DSN", "ESHU_CONTENT_STORE_DSN"}, Description: "Postgres connection string. DSN precedence is ESHU_FACT_STORE_DSN, then ESHU_CONTENT_STORE_DSN, then ESHU_POSTGRES_DSN."},
	{Name: "ESHU_POSTGRES_MAX_OPEN_CONNS", Type: VarInt, Default: "30", Subsystem: "postgres", Description: "Maximum open Postgres connections."},
	{Name: "ESHU_POSTGRES_MAX_IDLE_CONNS", Type: VarInt, Default: "10", Subsystem: "postgres", Description: "Maximum idle Postgres connections (capped at max open)."},
	{Name: "ESHU_POSTGRES_CONN_MAX_LIFETIME", Type: VarDuration, Default: "30m", Subsystem: "postgres", Description: "Connection lifetime before recycling."},
	{Name: "ESHU_POSTGRES_CONN_MAX_IDLE_TIME", Type: VarDuration, Default: "10m", Subsystem: "postgres", Description: "Idle timeout before a connection is closed."},
	{Name: "ESHU_POSTGRES_PING_TIMEOUT", Type: VarDuration, Default: "10s", Subsystem: "postgres", Description: "Timeout for the startup/readiness connectivity ping."},

	// graph
	{Name: "ESHU_GRAPH_BACKEND", Type: VarEnum, Default: "nornicdb", Subsystem: "graph", Allowed: []string{"neo4j", "nornicdb"}, Description: "Graph database backend."},
	{Name: "ESHU_NEO4J_URI", Type: VarString, Subsystem: "graph", Aliases: []string{"NEO4J_URI"}, Description: "Graph backend Bolt URI (falls back to NEO4J_URI)."},
	{Name: "ESHU_NEO4J_USERNAME", Type: VarString, Subsystem: "graph", Aliases: []string{"NEO4J_USERNAME"}, Description: "Graph backend username (falls back to NEO4J_USERNAME)."},
	{Name: "ESHU_NEO4J_PASSWORD", Type: VarString, Subsystem: "graph", Aliases: []string{"NEO4J_PASSWORD"}, Description: "Graph backend password (falls back to NEO4J_PASSWORD)."},
	{Name: "ESHU_NEO4J_DATABASE", Type: VarString, Subsystem: "graph", Aliases: []string{"NEO4J_DATABASE"}, Description: "Graph backend database name; defaults to neo4j for neo4j and nornic for nornicdb."},
	{Name: "ESHU_NEO4J_MAX_CONNECTION_POOL_SIZE", Type: VarInt, Default: "100", Subsystem: "graph", Description: "Maximum graph driver connection pool size."},
	{Name: "ESHU_NEO4J_MAX_CONNECTION_LIFETIME", Type: VarDuration, Default: "1h", Subsystem: "graph", Description: "Graph connection lifetime before pool recycling."},
	{Name: "ESHU_NEO4J_CONNECTION_ACQUISITION_TIMEOUT", Type: VarDuration, Default: "1m", Subsystem: "graph", Description: "Timeout for acquiring a graph connection from the pool."},
	{Name: "ESHU_NEO4J_SOCKET_CONNECT_TIMEOUT", Type: VarDuration, Default: "5s", Subsystem: "graph", Description: "Graph backend TCP socket connect timeout."},
	{Name: "ESHU_NEO4J_VERIFY_TIMEOUT", Type: VarDuration, Default: "10s", Subsystem: "graph", Description: "Timeout for graph driver connectivity verification."},

	// runtime
	{Name: "ESHU_LISTEN_ADDR", Type: VarString, Default: "0.0.0.0:8080", Subsystem: "runtime", Description: "Primary HTTP listen address (host:port)."},
	{Name: "ESHU_METRICS_ADDR", Type: VarString, Default: "0.0.0.0:9464", Subsystem: "runtime", Description: "Prometheus metrics listen address (host:port)."},
	{Name: "ESHU_PPROF_ADDR", Type: VarString, Subsystem: "runtime", Description: "Opt-in pprof profiler address; unset disables it; a port-only value binds to 127.0.0.1."},

	// api
	{Name: "ESHU_API_ADDR", Type: VarString, Default: ":8080", Subsystem: "api", Description: "API server listen address."},
	{Name: "ESHU_API_KEY", Type: VarString, Subsystem: "api", Description: "Bearer token for API authentication."},
	{Name: "ESHU_AUTH_OIDC_CONFIG_FILE", Type: VarString, Subsystem: "api", Description: "Path to an operator-managed OIDC login config file. When set and not disabled, the API enables backend Authorization Code login and reads provider/client/group-role mapping handles from this file."},
	{Name: "ESHU_AUTH_OIDC_ENABLED", Type: VarBool, Default: "false", Subsystem: "api", Description: "Explicitly enables or disables backend OIDC login. Set true with ESHU_AUTH_OIDC_CONFIG_FILE to require OIDC startup config; set false to disable even when a config file is present."},
	{Name: "ESHU_AUTH_OIDC_PROVIDER_ID", Type: VarString, Subsystem: "api", Description: "Optional default provider config id override for OIDC login. The id must reference a provider declared in ESHU_AUTH_OIDC_CONFIG_FILE."},
	{Name: "ESHU_AUTH_OIDC_SESSION_REFRESH_BATCH_SIZE", Type: VarInt, Default: "200", Subsystem: "api", Description: "Maximum OIDC-backed browser sessions processed per bounded active-session revocation refresh pass. Keeps each pass proportional to the batch rather than the full session table. Non-positive values fail API startup closed."},
	{Name: "ESHU_AUTH_OIDC_SESSION_REFRESH_ENABLED", Type: VarBool, Default: "false", Subsystem: "api", Description: "Enables the background OIDC active-session revocation refresh worker that re-resolves provider/user state for already-issued sessions within the staleness window and revokes sessions whose grants, role targets, or external subject are no longer valid."},
	{Name: "ESHU_AUTH_OIDC_SESSION_REFRESH_INTERVAL", Type: VarDuration, Default: "1m", Subsystem: "api", Description: "Cadence of the bounded OIDC active-session revocation refresh worker. Non-positive durations fail API startup closed."},
	{Name: "ESHU_AUTH_OIDC_SESSION_REFRESH_WINDOW", Type: VarDuration, Default: "15m", Subsystem: "api", Description: "Maximum staleness window for OIDC-backed browser sessions before the API revokes the session and requires fresh IdP reauthentication. Explicit invalid or non-positive durations fail API startup closed."},
	{Name: "ESHU_AUTH_OIDC_STATE_TTL", Type: VarDuration, Default: "10m", Subsystem: "api", Description: "OIDC login state and nonce lifetime. Explicit invalid durations fail API startup closed."},
	{Name: "ESHU_AUTH_OIDC_LOGIN_RATE_PER_SEC", Type: VarInt, Default: "10", Subsystem: "api", Description: "Maximum OIDC login requests per second per client IP. Requests exceeding this limit receive HTTP 429."},
	{Name: "ESHU_AUTH_OIDC_LOGIN_RATE_BURST", Type: VarInt, Default: "20", Subsystem: "api", Description: "Maximum burst size for the per-IP OIDC login rate limiter."},
	{Name: "ESHU_AUTH_OIDC_LOGIN_USER_RATE_PER_MIN", Type: VarInt, Default: "60", Subsystem: "api", Description: "Maximum OIDC login requests per minute per user (by provider_config_id). Requests exceeding this limit receive HTTP 429."},
	{Name: "ESHU_AUTH_OIDC_LOGIN_USER_BURST", Type: VarInt, Default: "10", Subsystem: "api", Description: "Maximum burst size for the per-user OIDC login rate limiter."},
	{Name: "ESHU_AUTO_GENERATE_API_KEY", Type: VarBool, Default: "false", Subsystem: "api", Description: "When true, auto-generate and persist an API key if none is set."},
	{Name: "ESHU_DISABLE_NEO4J", Type: VarBool, Default: "false", Subsystem: "api", Description: "When true, disable the graph backend entirely."},
	{Name: "ESHU_HOME", Type: VarString, Subsystem: "api", Description: "Root directory for persisted API key and configuration (defaults to ~/.eshu)."},
	{Name: "ESHU_QUERY_PROFILE", Type: VarEnum, Default: "production", Subsystem: "api", Allowed: []string{"production", "local_authoritative", "local_lightweight"}, Description: "Query execution profile."},
	{Name: "ESHU_SCOPED_TOKENS_FILE", Type: VarString, Subsystem: "api", Description: "Path to an operator-managed scoped-token registry file; API and MCP fail closed if the configured file is malformed or unreadable."},
	{Name: "ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_READ", Type: VarBool, Default: "false", Subsystem: "api", Description: "When true, serve GET /api/v0/supply-chain/impact/findings from the maintained canonical winners read model (#3389) instead of read-time dedup (bounded O(page)). Honored by both the API and MCP server; enable only after the reducer maintainer has populated the winners table. Output is byte-identical."},

	// mcp
	{Name: "ESHU_MCP_TRANSPORT", Type: VarEnum, Default: "http", Subsystem: "mcp", Allowed: []string{"http", "stdio"}, Description: "MCP server transport mode."},
	{Name: "ESHU_MCP_ADDR", Type: VarString, Default: ":8080", Subsystem: "mcp", Description: "MCP HTTP transport listen address."},

	// reducer
	{Name: "ESHU_CODE_VALUE_FLOW_STALE_CLEANUP_DELETE_BATCH_LIMIT", Type: VarInt, Default: "500", Subsystem: "reducer", Description: "Maximum stale value-flow evidence nodes or edges deleted per active scope and family in one reducer cleanup pass."},
	{Name: "ESHU_CODE_VALUE_FLOW_STALE_CLEANUP_ENABLED", Type: VarBool, Default: "true", Subsystem: "reducer", Description: "Enable the reducer side runner that removes stale CodeTaintEvidence nodes and TAINT_FLOWS_TO edges from older active-scope generations."},
	{Name: "ESHU_CODE_VALUE_FLOW_STALE_CLEANUP_LEASE_OWNER", Type: VarString, Subsystem: "reducer", Description: "Lease owner for the single value-flow stale cleanup worker; defaults to a unique process token."},
	{Name: "ESHU_CODE_VALUE_FLOW_STALE_CLEANUP_LEASE_TTL", Type: VarDuration, Default: "5m", Subsystem: "reducer", Description: "TTL for the value-flow stale cleanup partition lease."},
	{Name: "ESHU_CODE_VALUE_FLOW_STALE_CLEANUP_POLL_INTERVAL", Type: VarDuration, Default: "1h", Subsystem: "reducer", Description: "Delay between value-flow stale cleanup passes."},
	{Name: "ESHU_CODE_VALUE_FLOW_STALE_CLEANUP_SCOPE_BATCH_LIMIT", Type: VarInt, Default: "100", Subsystem: "reducer", Description: "Active repository scopes scanned per value-flow stale cleanup pass."},
	{Name: "ESHU_GRAPH_ORPHAN_SWEEP_BATCH_LIMIT", Type: VarInt, Default: "100", Subsystem: "reducer", Description: "Maximum graph orphan nodes deleted per label in one sweep pass."},
	{Name: "ESHU_GRAPH_ORPHAN_SWEEP_COUNT_LIMIT", Type: VarInt, Default: "10000", Subsystem: "reducer", Description: "Maximum graph orphan nodes counted per label for telemetry in one sweep pass."},
	{Name: "ESHU_GRAPH_ORPHAN_SWEEP_ENABLED", Type: VarBool, Default: "true", Subsystem: "reducer", Description: "Enable the reducer side runner that marks and sweeps stale generation-owned graph orphans."},
	{Name: "ESHU_GRAPH_ORPHAN_SWEEP_LEASE_OWNER", Type: VarString, Subsystem: "reducer", Description: "Lease owner for the single graph orphan sweep worker; defaults to a unique process token."},
	{Name: "ESHU_GRAPH_ORPHAN_SWEEP_LEASE_TTL", Type: VarDuration, Default: "5m", Subsystem: "reducer", Description: "TTL for the graph orphan sweep partition lease."},
	{Name: "ESHU_GRAPH_ORPHAN_SWEEP_POLL_INTERVAL", Type: VarDuration, Default: "1h", Subsystem: "reducer", Description: "Delay between graph orphan sweep passes."},
	{Name: "ESHU_GRAPH_ORPHAN_SWEEP_TTL", Type: VarDuration, Default: "168h", Subsystem: "reducer", Description: "Minimum age before a marked graph orphan can be deleted."},
	{Name: "ESHU_REDUCER_ADMISSION_HIGH_WATER_MARK", Type: VarInt, Default: "10000", Subsystem: "reducer", Description: "Ingester source-local reducer-intent admission threshold; defers while outstanding reducer queue depth is at or above this value. Set to 0 to disable."},
	{Name: "ESHU_REDUCER_ADMISSION_POLL_INTERVAL", Type: VarDuration, Default: "1s", Subsystem: "reducer", Description: "Queue-depth recheck interval while reducer admission is deferring."},
	{Name: "ESHU_REDUCER_ADMISSION_RETRYING_HIGH_WATER_MARK", Type: VarInt, Default: "500", Subsystem: "reducer", Description: "Ingester graph-write backpressure: defers source-local reducer-intent admission while retrying-state reducer depth (the durable signal of recurring graph-write timeouts) is at or above this value, so recoverable work is throttled instead of dead-lettered. Set to 0 to disable."},
	{Name: "ESHU_REDUCER_ADMISSION_RETRYING_LOW_WATER_MARK", Type: VarInt, Default: "100", Subsystem: "reducer", Description: "Hysteresis floor for graph-write backpressure; admission resumes only after retrying-state reducer depth falls below this value. Must be less than ESHU_REDUCER_ADMISSION_RETRYING_HIGH_WATER_MARK."},
	{Name: "ESHU_REDUCER_BATCH_CLAIM_SIZE", Type: VarInt, Subsystem: "reducer", Description: "Work items claimed per cycle (default adaptive to workers and backend)."},
	{Name: "ESHU_REDUCER_CLAIM_DOMAIN", Type: VarString, Subsystem: "reducer", Deprecated: true, ReplacedBy: "ESHU_REDUCER_CLAIM_DOMAINS", Description: "Single reducer claim domain."},
	{Name: "ESHU_REDUCER_CLAIM_DOMAINS", Type: VarString, Subsystem: "reducer", Description: "Comma-separated reducer domains for multi-domain claims."},
	{Name: "ESHU_REDUCER_MAX_ATTEMPTS", Type: VarInt, Default: "3", Subsystem: "reducer", Description: "Maximum retry attempts for reducer work items."},
	{Name: "ESHU_REDUCER_RETRY_DELAY", Type: VarDuration, Default: "30s", Subsystem: "reducer", Description: "Delay between reducer work-item retries."},
	{Name: "ESHU_REDUCER_WORKERS", Type: VarInt, Subsystem: "reducer", Description: "Concurrent reducer workers (default derived from CPU count and backend)."},

	// projector
	{Name: "ESHU_PROJECTOR_WORKERS", Type: VarInt, Subsystem: "projector", Description: "Concurrent projector workers (default NumCPU capped at 8, min 1)."},
	{Name: "ESHU_PROJECTOR_MAX_ATTEMPTS", Type: VarInt, Default: "3", Subsystem: "projector", Description: "Maximum retry attempts for projector work items."},
	{Name: "ESHU_PROJECTOR_RETRY_DELAY", Type: VarDuration, Default: "30s", Subsystem: "projector", Description: "Delay between projector work-item retries."},

	// coordinator
	{Name: "ESHU_WORKFLOW_COORDINATOR_DEPLOYMENT_MODE", Type: VarEnum, Default: "dark", Subsystem: "coordinator", Allowed: []string{"dark", "active"}, Description: "Workflow coordinator deployment mode."},
	{Name: "ESHU_WORKFLOW_COORDINATOR_CLAIMS_ENABLED", Type: VarBool, Default: "false", Subsystem: "coordinator", Aliases: []string{"ESHU_WORKFLOW_COORDINATOR_ENABLE_CLAIMS"}, Description: "Enable claim-based workflow coordination."},
	{Name: "ESHU_WORKFLOW_COORDINATOR_RECONCILE_INTERVAL", Type: VarDuration, Default: "30s", Subsystem: "coordinator", Description: "Workflow state reconciliation interval."},
	{Name: "ESHU_WORKFLOW_COORDINATOR_RUN_RECONCILE_INTERVAL", Type: VarDuration, Default: "30s", Subsystem: "coordinator", Description: "Run-level reconciliation interval."},
	{Name: "ESHU_WORKFLOW_COORDINATOR_REAP_INTERVAL", Type: VarDuration, Subsystem: "coordinator", Description: "Expired-claim reaping interval."},
	{Name: "ESHU_WORKFLOW_COORDINATOR_CLAIM_LEASE_TTL", Type: VarDuration, Subsystem: "coordinator", Description: "TTL for workflow claim leases."},
	{Name: "ESHU_WORKFLOW_COORDINATOR_HEARTBEAT_INTERVAL", Type: VarDuration, Subsystem: "coordinator", Description: "Heartbeat interval for claim owners."},
	{Name: "ESHU_WORKFLOW_COORDINATOR_EXPIRED_CLAIM_LIMIT", Type: VarInt, Subsystem: "coordinator", Description: "Reap batch limit for expired claims per pass."},
	{Name: "ESHU_WORKFLOW_COORDINATOR_EXPIRED_CLAIM_REQUEUE_DELAY", Type: VarDuration, Subsystem: "coordinator", Description: "Delay before requeuing expired claims."},
	{Name: "ESHU_WORKFLOW_COORDINATOR_TENANT_BOUNDARY_JSON", Type: VarString, Subsystem: "coordinator", Description: "JSON tenant boundary configuration."},
	{Name: "ESHU_COLLECTOR_INSTANCES_JSON", Type: VarString, Subsystem: "coordinator", Description: "JSON array of desired collector instances reconciled by the coordinator."},
	{Name: "ESHU_HOSTED_COLLECTOR_EGRESS_POLICY_JSON", Type: VarString, Subsystem: "coordinator", Description: "JSON egress policy applied to hosted collectors."},
	{Name: "ESHU_HOSTED_EXTENSION_EGRESS_POLICY_JSON", Type: VarString, Subsystem: "coordinator", Description: "JSON egress policy applied to hosted extensions."},

	// semantic
	{Name: "ESHU_SEMANTIC_EXTRACTION_POLICY_JSON", Type: VarString, Subsystem: "semantic", Description: "JSON semantic extraction policy controlling source/provider ACL decisions."},
	{Name: "ESHU_SEMANTIC_PROVIDER_EXECUTION_ENABLED", Type: VarBool, Default: "false", Subsystem: "semantic", Description: "Default-off flag permitting real provider traffic (requires security review)."},
	{Name: "ESHU_SEMANTIC_PROVIDER_PROFILES_JSON", Type: VarString, Subsystem: "semantic", Description: "JSON array of semantic provider profile configurations, including optional search embedding dimensions."},
	{Name: "ESHU_SEMANTIC_PROVIDER_WORKER_ENABLED", Type: VarBool, Default: "false", Subsystem: "semantic", Description: "Enable the semantic-provider worker claim loop."},
	{Name: "ESHU_SEMANTIC_SEARCH_LOCAL_EMBEDDER", Type: VarEnum, Subsystem: "semantic", Allowed: []string{"hash", "local_hash", "auto_hash"}, Description: "Deterministic no-network or auto-local semantic search selector for API, MCP, and reducer."},
	{Name: "ESHU_SEMANTIC_SEARCH_PROVIDER_PROFILE_ID", Type: VarString, Subsystem: "semantic", Description: "Selects one governed search_documents provider profile when multiple semantic search providers are configured."},

	// component
	{Name: "ESHU_COMPONENT_HOME", Type: VarString, Subsystem: "component", Description: "Root directory for the component/extension registry."},
	{Name: "ESHU_COMPONENT_TRUST_MODE", Type: VarString, Subsystem: "component", Description: "Component provenance verification mode."},
	{Name: "ESHU_COMPONENT_ALLOW_IDS", Type: VarString, Subsystem: "component", Description: "Comma-separated allowlist of component IDs."},
	{Name: "ESHU_COMPONENT_ALLOW_PUBLISHERS", Type: VarString, Subsystem: "component", Description: "Comma-separated allowlist of component publishers."},
	{Name: "ESHU_COMPONENT_REVOKE_IDS", Type: VarString, Subsystem: "component", Description: "Comma-separated revoke list of component IDs."},
	{Name: "ESHU_COMPONENT_REVOKE_PUBLISHERS", Type: VarString, Subsystem: "component", Description: "Comma-separated revoke list of component publishers."},
	{Name: "ESHU_COMPONENT_CORE_VERSION", Type: VarString, Subsystem: "component", Description: "Required core version for component compatibility."},
}
