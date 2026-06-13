# Environment Variable Reference

<!-- Generated from go/internal/envregistry. Do not edit by hand; regenerate with `ESHU_UPDATE_ENV_DOC=1 go test ./internal/envregistry -run TestEnvRegistryReferenceDocUpToDate`. -->

This reference is generated from the code-owned registry in `go/internal/envregistry`. It covers the core platform subsystems. Run `eshu config validate` to check the current environment against it.

## api

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_API_ADDR` | string | `:8080` | API server listen address. |
| `ESHU_API_KEY` | string | — | Bearer token for API authentication. |
| `ESHU_AUTO_GENERATE_API_KEY` | bool | `false` | When true, auto-generate and persist an API key if none is set. |
| `ESHU_DISABLE_NEO4J` | bool | `false` | When true, disable the graph backend entirely. |
| `ESHU_HOME` | string | — | Root directory for persisted API key and configuration (defaults to ~/.eshu). |
| `ESHU_QUERY_PROFILE` | enum | `production` | Query execution profile. Allowed: `production`, `local_authoritative`, `local_lightweight`. |

## component

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_COMPONENT_ALLOW_IDS` | string | — | Comma-separated allowlist of component IDs. |
| `ESHU_COMPONENT_ALLOW_PUBLISHERS` | string | — | Comma-separated allowlist of component publishers. |
| `ESHU_COMPONENT_CORE_VERSION` | string | — | Required core version for component compatibility. |
| `ESHU_COMPONENT_HOME` | string | — | Root directory for the component/extension registry. |
| `ESHU_COMPONENT_REVOKE_IDS` | string | — | Comma-separated revoke list of component IDs. |
| `ESHU_COMPONENT_REVOKE_PUBLISHERS` | string | — | Comma-separated revoke list of component publishers. |
| `ESHU_COMPONENT_TRUST_MODE` | string | — | Component provenance verification mode. |

## coordinator

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_COLLECTOR_INSTANCES_JSON` | string | — | JSON array of desired collector instances reconciled by the coordinator. |
| `ESHU_HOSTED_COLLECTOR_EGRESS_POLICY_JSON` | string | — | JSON egress policy applied to hosted collectors. |
| `ESHU_HOSTED_EXTENSION_EGRESS_POLICY_JSON` | string | — | JSON egress policy applied to hosted extensions. |
| `ESHU_WORKFLOW_COORDINATOR_CLAIMS_ENABLED` | bool | `false` | Enable claim-based workflow coordination. Aliases: `ESHU_WORKFLOW_COORDINATOR_ENABLE_CLAIMS`. |
| `ESHU_WORKFLOW_COORDINATOR_CLAIM_LEASE_TTL` | duration | — | TTL for workflow claim leases. |
| `ESHU_WORKFLOW_COORDINATOR_DEPLOYMENT_MODE` | enum | `dark` | Workflow coordinator deployment mode. Allowed: `dark`, `active`. |
| `ESHU_WORKFLOW_COORDINATOR_EXPIRED_CLAIM_LIMIT` | int | — | Reap batch limit for expired claims per pass. |
| `ESHU_WORKFLOW_COORDINATOR_EXPIRED_CLAIM_REQUEUE_DELAY` | duration | — | Delay before requeuing expired claims. |
| `ESHU_WORKFLOW_COORDINATOR_HEARTBEAT_INTERVAL` | duration | — | Heartbeat interval for claim owners. |
| `ESHU_WORKFLOW_COORDINATOR_REAP_INTERVAL` | duration | — | Expired-claim reaping interval. |
| `ESHU_WORKFLOW_COORDINATOR_RECONCILE_INTERVAL` | duration | `30s` | Workflow state reconciliation interval. |
| `ESHU_WORKFLOW_COORDINATOR_RUN_RECONCILE_INTERVAL` | duration | `30s` | Run-level reconciliation interval. |
| `ESHU_WORKFLOW_COORDINATOR_TENANT_BOUNDARY_JSON` | string | — | JSON tenant boundary configuration. |

## graph

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_GRAPH_BACKEND` | enum | `nornicdb` | Graph database backend. Allowed: `neo4j`, `nornicdb`. |
| `ESHU_NEO4J_CONNECTION_ACQUISITION_TIMEOUT` | duration | `1m` | Timeout for acquiring a graph connection from the pool. |
| `ESHU_NEO4J_DATABASE` | string | — | Graph backend database name; defaults to neo4j for neo4j and nornic for nornicdb. Aliases: `NEO4J_DATABASE`. |
| `ESHU_NEO4J_MAX_CONNECTION_LIFETIME` | duration | `1h` | Graph connection lifetime before pool recycling. |
| `ESHU_NEO4J_MAX_CONNECTION_POOL_SIZE` | int | `100` | Maximum graph driver connection pool size. |
| `ESHU_NEO4J_PASSWORD` | string | — | Graph backend password (falls back to NEO4J_PASSWORD). Aliases: `NEO4J_PASSWORD`. |
| `ESHU_NEO4J_SOCKET_CONNECT_TIMEOUT` | duration | `5s` | Graph backend TCP socket connect timeout. |
| `ESHU_NEO4J_URI` | string | — | Graph backend Bolt URI (falls back to NEO4J_URI). Aliases: `NEO4J_URI`. |
| `ESHU_NEO4J_USERNAME` | string | — | Graph backend username (falls back to NEO4J_USERNAME). Aliases: `NEO4J_USERNAME`. |
| `ESHU_NEO4J_VERIFY_TIMEOUT` | duration | `10s` | Timeout for graph driver connectivity verification. |

## mcp

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_MCP_ADDR` | string | `:8080` | MCP HTTP transport listen address. |
| `ESHU_MCP_TRANSPORT` | enum | `http` | MCP server transport mode. Allowed: `http`, `stdio`. |

## postgres

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_POSTGRES_CONN_MAX_IDLE_TIME` | duration | `10m` | Idle timeout before a connection is closed. |
| `ESHU_POSTGRES_CONN_MAX_LIFETIME` | duration | `30m` | Connection lifetime before recycling. |
| `ESHU_POSTGRES_DSN` | dsn | — | Postgres connection string. DSN precedence is ESHU_FACT_STORE_DSN, then ESHU_CONTENT_STORE_DSN, then ESHU_POSTGRES_DSN. Aliases: `ESHU_FACT_STORE_DSN`, `ESHU_CONTENT_STORE_DSN`. |
| `ESHU_POSTGRES_MAX_IDLE_CONNS` | int | `10` | Maximum idle Postgres connections (capped at max open). |
| `ESHU_POSTGRES_MAX_OPEN_CONNS` | int | `30` | Maximum open Postgres connections. |
| `ESHU_POSTGRES_PING_TIMEOUT` | duration | `10s` | Timeout for the startup/readiness connectivity ping. |

## projector

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_PROJECTOR_MAX_ATTEMPTS` | int | `3` | Maximum retry attempts for projector work items. |
| `ESHU_PROJECTOR_RETRY_DELAY` | duration | `30s` | Delay between projector work-item retries. |
| `ESHU_PROJECTOR_WORKERS` | int | — | Concurrent projector workers (default NumCPU capped at 8, min 1). |

## reducer

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_REDUCER_BATCH_CLAIM_SIZE` | int | — | Work items claimed per cycle (default adaptive to workers and backend). |
| `ESHU_REDUCER_CLAIM_DOMAIN` | string | — | Single reducer claim domain. Deprecated; use `ESHU_REDUCER_CLAIM_DOMAINS`. |
| `ESHU_REDUCER_CLAIM_DOMAINS` | string | — | Comma-separated reducer domains for multi-domain claims. |
| `ESHU_REDUCER_MAX_ATTEMPTS` | int | `3` | Maximum retry attempts for reducer work items. |
| `ESHU_REDUCER_RETRY_DELAY` | duration | `30s` | Delay between reducer work-item retries. |
| `ESHU_REDUCER_WORKERS` | int | — | Concurrent reducer workers (default derived from CPU count and backend). |

## runtime

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_LISTEN_ADDR` | string | `0.0.0.0:8080` | Primary HTTP listen address (host:port). |
| `ESHU_METRICS_ADDR` | string | `0.0.0.0:9464` | Prometheus metrics listen address (host:port). |
| `ESHU_PPROF_ADDR` | string | — | Opt-in pprof profiler address; unset disables it; a port-only value binds to 127.0.0.1. |

## semantic

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_SEMANTIC_EXTRACTION_POLICY_JSON` | string | — | JSON semantic extraction policy controlling source/provider ACL decisions. |
| `ESHU_SEMANTIC_PROVIDER_EXECUTION_ENABLED` | bool | `false` | Default-off flag permitting real provider traffic (requires security review). |
| `ESHU_SEMANTIC_PROVIDER_PROFILES_JSON` | string | — | JSON array of semantic provider profile configurations. |
| `ESHU_SEMANTIC_PROVIDER_WORKER_ENABLED` | bool | `false` | Enable the semantic-provider worker claim loop. |

