# Environment Variable Reference

<!-- Generated from go/internal/envregistry. Do not edit by hand; regenerate with `ESHU_UPDATE_ENV_DOC=1 go test ./internal/envregistry -run TestEnvRegistryReferenceDocUpToDate`. -->

This reference is generated from the code-owned registry in `go/internal/envregistry`. It covers the core platform subsystems. Run `eshu config validate` to check the current environment against it.

## api

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_API_ADDR` | string | `:8080` | API server listen address. |
| `ESHU_API_KEY` | string | — | Bearer token for API authentication. |
| `ESHU_AUTH_OIDC_CONFIG_FILE` | string | — | Path to an operator-managed OIDC login config file. When set and not disabled, the API enables backend Authorization Code login and reads provider/client/group-role mapping handles from this file. |
| `ESHU_AUTH_OIDC_ENABLED` | bool | `false` | Explicitly enables or disables backend OIDC login. Set true with ESHU_AUTH_OIDC_CONFIG_FILE to require OIDC startup config; set false to disable even when a config file is present. |
| `ESHU_AUTH_OIDC_PROVIDER_ID` | string | — | Optional default provider config id override for OIDC login. The id must reference a provider declared in ESHU_AUTH_OIDC_CONFIG_FILE. |
| `ESHU_AUTH_OIDC_STATE_TTL` | duration | `10m` | OIDC login state and nonce lifetime. Explicit invalid durations fail API startup closed. |
| `ESHU_AUTO_GENERATE_API_KEY` | bool | `false` | When true, auto-generate and persist an API key if none is set. |
| `ESHU_DISABLE_NEO4J` | bool | `false` | When true, disable the graph backend entirely. |
| `ESHU_HOME` | string | — | Root directory for persisted API key and configuration (defaults to ~/.eshu). |
| `ESHU_QUERY_PROFILE` | enum | `production` | Query execution profile. Allowed: `production`, `local_authoritative`, `local_lightweight`. |
| `ESHU_SCOPED_TOKENS_FILE` | string | — | Path to an operator-managed scoped-token registry file; API and MCP fail closed if the configured file is malformed or unreadable. |
| `ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_READ` | bool | `false` | When true, serve GET /api/v0/supply-chain/impact/findings from the maintained canonical winners read model (#3389) instead of read-time dedup (bounded O(page)). Honored by both the API and MCP server; enable only after the reducer maintainer has populated the winners table. Output is byte-identical. |

## collector-aws-cloud

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_AWS_COLLECTOR_CLAIM_LEASE_TTL` | duration | `60s` | Workflow claim lease TTL. |
| `ESHU_AWS_COLLECTOR_HEARTBEAT_INTERVAL` | duration | `20s` | Claim heartbeat interval; must be less than the lease TTL. |
| `ESHU_AWS_COLLECTOR_INSTANCE_ID` | string | — | Instance ID selecting this collector instance from ESHU_COLLECTOR_INSTANCES_JSON. |
| `ESHU_AWS_COLLECTOR_OWNER_ID` | string | — | Lease owner identifier; defaults to the hostname. |
| `ESHU_AWS_COLLECTOR_POLL_INTERVAL` | duration | `1s` | Poll interval for discovering targets. |
| `ESHU_AWS_REDACTION_KEY` | string | — | Encryption key for redacting AWS secrets when sensitive service scans are enabled. |

## collector-azure-cloud

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_AZURE_COLLECTOR_CLAIM_LEASE_TTL` | duration | — | Workflow claim lease TTL used by claimed-live mode; defaults to the workflow default. |
| `ESHU_AZURE_COLLECTOR_HEARTBEAT_INTERVAL` | duration | — | Claim heartbeat interval for claimed-live mode; must be less than the claim lease TTL. |
| `ESHU_AZURE_COLLECTOR_INSTANCE_ID` | string | — | Instance ID selecting this Azure collector instance. |
| `ESHU_AZURE_COLLECTOR_OWNER_ID` | string | — | Lease owner identifier for claimed-live mode; defaults to the hostname. |
| `ESHU_AZURE_FIXTURE_PAGES_JSON` | string | — | JSON fixture pages for single-lane offline Resource Graph or resourcechanges smoke testing; not used in production. |
| `ESHU_AZURE_POLL_INTERVAL` | duration | `5m` | Poll interval for discovering Azure targets. |
| `ESHU_AZURE_REDACTION_KEY_FILE` | string | — | Read-only file path for Azure redaction key material used to fingerprint tags, managed identities, and resource-change actors. |
| `ESHU_AZURE_TARGETS_JSON` | string | — | JSON array of Azure target scopes. Each target may set source_lane to resource_graph or fixture-only resource_changes. |

## collector-cicd-run

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_CICD_RUN_CLAIM_LEASE_TTL` | duration | `60s` | Workflow claim lease TTL. |
| `ESHU_CICD_RUN_COLLECTOR_INSTANCE_ID` | string | — | Instance ID selecting this collector instance from ESHU_COLLECTOR_INSTANCES_JSON. |
| `ESHU_CICD_RUN_COLLECTOR_OWNER_ID` | string | — | Lease owner identifier; defaults to the hostname. |
| `ESHU_CICD_RUN_HEARTBEAT_INTERVAL` | duration | `20s` | Claim heartbeat interval; must be less than the lease TTL. |
| `ESHU_CICD_RUN_POLL_INTERVAL` | duration | `1s` | Poll interval for discovering targets. |

## collector-component-extension

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_COMPONENT_COLLECTOR_CLAIM_LEASE_TTL` | duration | `60s` | Workflow claim lease TTL. |
| `ESHU_COMPONENT_COLLECTOR_HEARTBEAT_INTERVAL` | duration | `20s` | Claim heartbeat interval; must be less than the lease TTL. |
| `ESHU_COMPONENT_COLLECTOR_INSTANCE_ID` | string | — | Instance ID for the component-extension collector. |
| `ESHU_COMPONENT_COLLECTOR_OWNER_ID` | string | — | Lease owner identifier; defaults to the hostname. |
| `ESHU_COMPONENT_COLLECTOR_POLL_INTERVAL` | duration | `1s` | Poll interval for discovering component activations. |
| `ESHU_COMPONENT_COLLECTOR_SCOPE_KIND` | string | — | Scope kind for component activations (e.g. organization, team). |

## collector-gcp-cloud

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_GCP_COLLECTOR_CLAIM_LEASE_TTL` | duration | `60s` | Workflow claim lease TTL. |
| `ESHU_GCP_COLLECTOR_HEARTBEAT_INTERVAL` | duration | `20s` | Claim heartbeat interval; must be less than the lease TTL. |
| `ESHU_GCP_COLLECTOR_INSTANCE_ID` | string | — | Instance ID selecting this collector instance from ESHU_COLLECTOR_INSTANCES_JSON. |
| `ESHU_GCP_COLLECTOR_OWNER_ID` | string | — | Lease owner identifier; defaults to the hostname. |
| `ESHU_GCP_COLLECTOR_POLL_INTERVAL` | duration | `1s` | Poll interval for discovering targets. |

## collector-grafana

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_GRAFANA_COLLECTOR_CLAIM_LEASE_TTL` | duration | `60s` | Workflow claim lease TTL. |
| `ESHU_GRAFANA_COLLECTOR_HEARTBEAT_INTERVAL` | duration | `20s` | Claim heartbeat interval; must be less than the lease TTL. |
| `ESHU_GRAFANA_COLLECTOR_INSTANCE_ID` | string | — | Instance ID selecting this collector instance from ESHU_COLLECTOR_INSTANCES_JSON. |
| `ESHU_GRAFANA_COLLECTOR_OWNER_ID` | string | — | Lease owner identifier; defaults to the hostname. |
| `ESHU_GRAFANA_COLLECTOR_POLL_INTERVAL` | duration | `1s` | Poll interval for discovering targets. |

## collector-jira

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_JIRA_CLAIM_LEASE_TTL` | duration | `60s` | Workflow claim lease TTL. |
| `ESHU_JIRA_COLLECTOR_INSTANCE_ID` | string | — | Instance ID selecting this collector instance from ESHU_COLLECTOR_INSTANCES_JSON. |
| `ESHU_JIRA_COLLECTOR_OWNER_ID` | string | — | Lease owner identifier; defaults to the hostname. |
| `ESHU_JIRA_HEARTBEAT_INTERVAL` | duration | `20s` | Claim heartbeat interval; must be less than the lease TTL. |
| `ESHU_JIRA_POLL_INTERVAL` | duration | `1s` | Poll interval for discovering targets. |

## collector-kubernetes-live

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_KUBERNETES_LIVE_CLUSTERS_JSON` | string | — | JSON array of Kubernetes cluster targets with auth configuration. |
| `ESHU_KUBERNETES_LIVE_COLLECTOR_INSTANCE_ID` | string | — | Instance ID for the kubernetes-live collector. |
| `ESHU_KUBERNETES_LIVE_POLL_INTERVAL` | duration | `5m` | Poll interval for discovering Kubernetes resources. |

## collector-loki

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_LOKI_COLLECTOR_CLAIM_LEASE_TTL` | duration | `60s` | Workflow claim lease TTL. |
| `ESHU_LOKI_COLLECTOR_HEARTBEAT_INTERVAL` | duration | `20s` | Claim heartbeat interval; must be less than the lease TTL. |
| `ESHU_LOKI_COLLECTOR_INSTANCE_ID` | string | — | Instance ID selecting this collector instance from ESHU_COLLECTOR_INSTANCES_JSON. |
| `ESHU_LOKI_COLLECTOR_OWNER_ID` | string | — | Lease owner identifier; defaults to the hostname. |
| `ESHU_LOKI_COLLECTOR_POLL_INTERVAL` | duration | `1s` | Poll interval for discovering targets. |

## collector-oci-registry

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_OCI_REGISTRY_CLAIM_LEASE_TTL` | duration | `60s` | Workflow claim lease TTL. |
| `ESHU_OCI_REGISTRY_COLLECTOR_INSTANCE_ID` | string | — | Instance ID selecting this collector instance from ESHU_COLLECTOR_INSTANCES_JSON. |
| `ESHU_OCI_REGISTRY_COLLECTOR_OWNER_ID` | string | — | Lease owner identifier; defaults to the hostname. |
| `ESHU_OCI_REGISTRY_HEARTBEAT_INTERVAL` | duration | `20s` | Claim heartbeat interval; must be less than the lease TTL. |
| `ESHU_OCI_REGISTRY_POLL_INTERVAL` | duration | `1s` | Poll interval for discovering targets. |
| `ESHU_OCI_REGISTRY_TARGETS_JSON` | string | — | JSON array of OCI registry target configurations. |

## collector-package-registry

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_PACKAGE_REGISTRY_CLAIM_LEASE_TTL` | duration | `60s` | Workflow claim lease TTL. |
| `ESHU_PACKAGE_REGISTRY_COLLECTOR_INSTANCE_ID` | string | — | Instance ID selecting this collector instance from ESHU_COLLECTOR_INSTANCES_JSON. |
| `ESHU_PACKAGE_REGISTRY_COLLECTOR_OWNER_ID` | string | — | Lease owner identifier; defaults to the hostname. |
| `ESHU_PACKAGE_REGISTRY_HEARTBEAT_INTERVAL` | duration | `20s` | Claim heartbeat interval; must be less than the lease TTL. |
| `ESHU_PACKAGE_REGISTRY_POLL_INTERVAL` | duration | `1s` | Poll interval for discovering targets. |

## collector-pagerduty

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_PAGERDUTY_CLAIM_LEASE_TTL` | duration | `60s` | Workflow claim lease TTL. |
| `ESHU_PAGERDUTY_COLLECTOR_INSTANCE_ID` | string | — | Instance ID selecting this collector instance from ESHU_COLLECTOR_INSTANCES_JSON. |
| `ESHU_PAGERDUTY_COLLECTOR_OWNER_ID` | string | — | Lease owner identifier; defaults to the hostname. |
| `ESHU_PAGERDUTY_HEARTBEAT_INTERVAL` | duration | `20s` | Claim heartbeat interval; must be less than the lease TTL. |
| `ESHU_PAGERDUTY_POLL_INTERVAL` | duration | `1s` | Poll interval for discovering targets. |

## collector-prometheus-mimir

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_PROMETHEUS_MIMIR_COLLECTOR_CLAIM_LEASE_TTL` | duration | `60s` | Workflow claim lease TTL. |
| `ESHU_PROMETHEUS_MIMIR_COLLECTOR_HEARTBEAT_INTERVAL` | duration | `20s` | Claim heartbeat interval; must be less than the lease TTL. |
| `ESHU_PROMETHEUS_MIMIR_COLLECTOR_INSTANCE_ID` | string | — | Instance ID selecting this collector instance from ESHU_COLLECTOR_INSTANCES_JSON. |
| `ESHU_PROMETHEUS_MIMIR_COLLECTOR_OWNER_ID` | string | — | Lease owner identifier; defaults to the hostname. |
| `ESHU_PROMETHEUS_MIMIR_COLLECTOR_POLL_INTERVAL` | duration | `1s` | Poll interval for discovering targets. |

## collector-sbom-attestation

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_SBOM_ATTESTATION_CLAIM_LEASE_TTL` | duration | `60s` | Workflow claim lease TTL. |
| `ESHU_SBOM_ATTESTATION_COLLECTOR_INSTANCE_ID` | string | — | Instance ID selecting this collector instance from ESHU_COLLECTOR_INSTANCES_JSON. |
| `ESHU_SBOM_ATTESTATION_COLLECTOR_OWNER_ID` | string | — | Lease owner identifier; defaults to the hostname. |
| `ESHU_SBOM_ATTESTATION_HEARTBEAT_INTERVAL` | duration | `20s` | Claim heartbeat interval; must be less than the lease TTL. |
| `ESHU_SBOM_ATTESTATION_POLL_INTERVAL` | duration | `1s` | Poll interval for discovering targets. |

## collector-security-alerts

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_SECURITY_ALERT_CLAIM_LEASE_TTL` | duration | `60s` | Workflow claim lease TTL. |
| `ESHU_SECURITY_ALERT_COLLECTOR_INSTANCE_ID` | string | — | Instance ID selecting this collector instance from ESHU_COLLECTOR_INSTANCES_JSON. |
| `ESHU_SECURITY_ALERT_COLLECTOR_OWNER_ID` | string | — | Lease owner identifier; defaults to the hostname. |
| `ESHU_SECURITY_ALERT_HEARTBEAT_INTERVAL` | duration | `20s` | Claim heartbeat interval; must be less than the lease TTL. |
| `ESHU_SECURITY_ALERT_POLL_INTERVAL` | duration | `1s` | Poll interval for discovering targets. |

## collector-tempo

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_TEMPO_COLLECTOR_CLAIM_LEASE_TTL` | duration | `60s` | Workflow claim lease TTL. |
| `ESHU_TEMPO_COLLECTOR_HEARTBEAT_INTERVAL` | duration | `20s` | Claim heartbeat interval; must be less than the lease TTL. |
| `ESHU_TEMPO_COLLECTOR_INSTANCE_ID` | string | — | Instance ID selecting this collector instance from ESHU_COLLECTOR_INSTANCES_JSON. |
| `ESHU_TEMPO_COLLECTOR_OWNER_ID` | string | — | Lease owner identifier; defaults to the hostname. |
| `ESHU_TEMPO_COLLECTOR_POLL_INTERVAL` | duration | `1s` | Poll interval for discovering targets. |

## collector-terraform-state

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_TERRAFORM_SCHEMA_DIR` | string | — | Directory of Terraform provider schemas; defaults to the built-in schema directory. |
| `ESHU_TFSTATE_COLLECTOR_CLAIM_LEASE_TTL` | duration | `60s` | Workflow claim lease TTL. |
| `ESHU_TFSTATE_COLLECTOR_HEARTBEAT` | duration | `20s` | Legacy heartbeat interval alias. Deprecated; use `ESHU_TFSTATE_COLLECTOR_HEARTBEAT_INTERVAL`. |
| `ESHU_TFSTATE_COLLECTOR_HEARTBEAT_INTERVAL` | duration | `20s` | Claim heartbeat interval; must be less than the lease TTL. |
| `ESHU_TFSTATE_COLLECTOR_INSTANCE_ID` | string | — | Instance ID selecting this collector instance from ESHU_COLLECTOR_INSTANCES_JSON. |
| `ESHU_TFSTATE_COLLECTOR_OWNER_ID` | string | — | Lease owner identifier; defaults to the hostname. |
| `ESHU_TFSTATE_COLLECTOR_POLL_INTERVAL` | duration | `1s` | Poll interval for discovering targets. |
| `ESHU_TFSTATE_REDACTION_KEY` | string | — | Encryption key for redacting Terraform state secrets. |
| `ESHU_TFSTATE_REDACTION_RULESET_VERSION` | string | — | Versioned policy identifier for redacting sensitive keys. |
| `ESHU_TFSTATE_REDACTION_SENSITIVE_KEYS` | string | — | Comma-separated leaf keys to redact; defaults to password,secret,token,access_key,private_key,certificate,key_pair. |
| `ESHU_TFSTATE_SOURCE_MAX_BYTES` | int | `0` | Maximum bytes accepted per Terraform state source; 0 means unlimited. |

## collector-vault-live

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_VAULT_LIVE_CLAIM_LEASE_TTL` | duration | `60s` | Workflow claim lease TTL. |
| `ESHU_VAULT_LIVE_COLLECTOR_INSTANCE_ID` | string | — | Instance ID selecting this collector instance from ESHU_COLLECTOR_INSTANCES_JSON. |
| `ESHU_VAULT_LIVE_COLLECTOR_OWNER_ID` | string | — | Lease owner identifier; defaults to the hostname. |
| `ESHU_VAULT_LIVE_HEARTBEAT_INTERVAL` | duration | `20s` | Claim heartbeat interval; must be less than the lease TTL. |
| `ESHU_VAULT_LIVE_POLL_INTERVAL` | duration | `1s` | Poll interval for discovering targets. |
| `ESHU_VAULT_LIVE_REDACTION_KEY` | string | — | Encryption key for redacting sensitive Vault data. |

## collector-vulnerability-intelligence

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_VULNERABILITY_INTELLIGENCE_CLAIM_LEASE_TTL` | duration | `60s` | Workflow claim lease TTL. |
| `ESHU_VULNERABILITY_INTELLIGENCE_COLLECTOR_INSTANCE_ID` | string | — | Instance ID selecting this collector instance from ESHU_COLLECTOR_INSTANCES_JSON. |
| `ESHU_VULNERABILITY_INTELLIGENCE_COLLECTOR_OWNER_ID` | string | — | Lease owner identifier; defaults to the hostname. |
| `ESHU_VULNERABILITY_INTELLIGENCE_HEARTBEAT_INTERVAL` | duration | `20s` | Claim heartbeat interval; must be less than the lease TTL. |
| `ESHU_VULNERABILITY_INTELLIGENCE_POLL_INTERVAL` | duration | `1s` | Poll interval for discovering targets. |

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
| `ESHU_CODE_VALUE_FLOW_STALE_CLEANUP_DELETE_BATCH_LIMIT` | int | `500` | Maximum stale value-flow evidence nodes or edges deleted per active scope and family in one reducer cleanup pass. |
| `ESHU_CODE_VALUE_FLOW_STALE_CLEANUP_ENABLED` | bool | `true` | Enable the reducer side runner that removes stale CodeTaintEvidence nodes and TAINT_FLOWS_TO edges from older active-scope generations. |
| `ESHU_CODE_VALUE_FLOW_STALE_CLEANUP_LEASE_OWNER` | string | — | Lease owner for the single value-flow stale cleanup worker; defaults to a unique process token. |
| `ESHU_CODE_VALUE_FLOW_STALE_CLEANUP_LEASE_TTL` | duration | `5m` | TTL for the value-flow stale cleanup partition lease. |
| `ESHU_CODE_VALUE_FLOW_STALE_CLEANUP_POLL_INTERVAL` | duration | `1h` | Delay between value-flow stale cleanup passes. |
| `ESHU_CODE_VALUE_FLOW_STALE_CLEANUP_SCOPE_BATCH_LIMIT` | int | `100` | Active repository scopes scanned per value-flow stale cleanup pass. |
| `ESHU_GRAPH_ORPHAN_SWEEP_BATCH_LIMIT` | int | `100` | Maximum graph orphan nodes deleted per label in one sweep pass. |
| `ESHU_GRAPH_ORPHAN_SWEEP_COUNT_LIMIT` | int | `10000` | Maximum graph orphan nodes counted per label for telemetry in one sweep pass. |
| `ESHU_GRAPH_ORPHAN_SWEEP_ENABLED` | bool | `true` | Enable the reducer side runner that marks and sweeps stale generation-owned graph orphans. |
| `ESHU_GRAPH_ORPHAN_SWEEP_LEASE_OWNER` | string | — | Lease owner for the single graph orphan sweep worker; defaults to a unique process token. |
| `ESHU_GRAPH_ORPHAN_SWEEP_LEASE_TTL` | duration | `5m` | TTL for the graph orphan sweep partition lease. |
| `ESHU_GRAPH_ORPHAN_SWEEP_POLL_INTERVAL` | duration | `1h` | Delay between graph orphan sweep passes. |
| `ESHU_GRAPH_ORPHAN_SWEEP_TTL` | duration | `168h` | Minimum age before a marked graph orphan can be deleted. |
| `ESHU_REDUCER_ADMISSION_HIGH_WATER_MARK` | int | `10000` | Ingester source-local reducer-intent admission threshold; defers while outstanding reducer queue depth is at or above this value. Set to 0 to disable. |
| `ESHU_REDUCER_ADMISSION_POLL_INTERVAL` | duration | `1s` | Queue-depth recheck interval while reducer admission is deferring. |
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
| `ESHU_SEMANTIC_PROVIDER_PROFILES_JSON` | string | — | JSON array of semantic provider profile configurations, including optional search embedding dimensions. |
| `ESHU_SEMANTIC_PROVIDER_WORKER_ENABLED` | bool | `false` | Enable the semantic-provider worker claim loop. |
| `ESHU_SEMANTIC_SEARCH_LOCAL_EMBEDDER` | enum | — | Deterministic no-network or auto-local semantic search selector for API, MCP, and reducer. Allowed: `hash`, `local_hash`, `auto_hash`. |
| `ESHU_SEMANTIC_SEARCH_PROVIDER_PROFILE_ID` | string | — | Selects one governed search_documents provider profile when multiple semantic search providers are configured. |

