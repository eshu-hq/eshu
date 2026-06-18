# Runtime And Storage Environment

This page covers variables read by core services, local service wrappers,
authentication, Postgres, Bolt graph adapters, telemetry, memory setup, and the
local installer.

## Core Runtime

| Variable | Default | Read by | Purpose |
| --- | --- | --- | --- |
| `ESHU_HOME` | Platform user-data dir | CLI, local Eshu service, API key resolver | Root for user config, local workspaces, managed binaries, and persisted local API keys. |
| `ESHU_COMPONENT_HOME` | unset for hosted component runtimes unless deployment config sets it; CLI falls back to `ESHU_HOME/components`, then `~/.eshu/components`; default Compose sets `/data/.eshu/components` for the workflow coordinator and component extension collector | CLI component package manager, API, MCP, workflow coordinator, component extension collector | Local component registry home. When set for API/MCP, it enables read-only component extension inventory and diagnostics. When set for hosted component runtimes, trusted claim-capable activations can become scheduled collector instances and process-backed collector claims. |
| `ESHU_QUERY_PROFILE` | `production` for deployed API/MCP/reducer; local commands set the owner profile (`eshu mcp start` defaults to `local_authoritative`, `eshu index` to `local_lightweight`) | API, MCP, ingester, reducer, local service | Selects query/runtime profile such as `production`, `local_lightweight`, or `local_authoritative`. Set explicitly to override the command default. |
| `ESHU_GRAPH_BACKEND` | `nornicdb` | API, MCP, ingester, reducer, local service | Selects graph adapter: `nornicdb` or `neo4j`. |
| `ESHU_LISTEN_ADDR` | `0.0.0.0:8080` | Go service runtimes | HTTP listen address for services using shared runtime config. |
| `ESHU_METRICS_ADDR` | `0.0.0.0:9464` | Go service runtimes | Prometheus metrics listen address. |
| `ESHU_PPROF_ADDR` | unset | API, MCP, ingester, reducer, bootstrap-index, workflow coordinator, hosted collectors | Opt-in `net/http/pprof` endpoint. A bare port binds to `127.0.0.1`; include a host to expose elsewhere. |
| `ESHU_API_ADDR` | `:8080` for `eshu-api` | API CLI service wrapper | API listen address for scripted service helpers. Prefer CLI flags for direct use. |
| `ESHU_MCP_TRANSPORT` | `http` | MCP server | MCP transport, `http` or `stdio`. |
| `ESHU_MCP_ADDR` | `:8080` | MCP server | HTTP MCP listen address. |
| `ESHU_SEMANTIC_PROVIDER_PROFILES_JSON` | unset | API, MCP | Optional semantic extraction provider profile registry. JSON must contain provider metadata and credential handles only; raw provider keys are rejected for environment-variable credential sources and must not be committed. Status output reports redacted profile state and source-policy gating without loading credentials or calling providers. |
| `ESHU_SEMANTIC_EXTRACTION_POLICY_JSON` | unset | API, MCP | Optional hosted semantic extraction policy. JSON must explicitly allow semantic provider egress plus provider profile ids, source classes, organization/tenant/project/repository scopes, source selectors, max chunk/token/cost limits, redaction mode, and retention posture. Without this policy or without semantic-provider egress allow, configured provider profiles remain visible but semantic extraction reports policy-disabled. |
| `ESHU_SEMANTIC_SEARCH_LOCAL_EMBEDDER` | unset | API, MCP | Explicitly enables deterministic no-network local semantic/hybrid retrieval for `POST /api/v0/search/semantic` and MCP `search_semantic_context` when set to `hash` or `local_hash`. Unset keeps semantic unavailable and hybrid BM25-degraded. When set, the route serves ready active persisted vector rows and reports explicit degraded state when rows are missing, stale, partial, rebuilding, failed, or incompatible. This reads active curated search documents and local vector sidecar rows only; it does not configure hosted providers, credentials, egress, graph writes, or external vector-store readiness. |
| `ESHU_WORKFLOW_COORDINATOR_TENANT_BOUNDARY_JSON` | unset | workflow coordinator | Optional hosted claim boundary. JSON must contain opaque `tenant_id`, `workspace_id`, `subject_class`, and `policy_revision_hash`; when set, coordinator planning intersects work-item scopes with active tenant/workspace grants before creating claimable rows. |
| `ESHU_RUNTIME_DB_TYPE` | CLI flag derived | CLI root command | Legacy CLI database selector. Prefer explicit graph/backend settings. |
| `ESHU_WATCH_PATH` | local service sets it | local child processes | Workspace path handed to local child processes. Do not set manually. |
| `ESHU_DISABLE_NEO4J` | unset | API, MCP, ingester, local service | Transitional local-lightweight skip flag. Prefer profile/backend settings. |

See [Semantic Enrichment Posture](semantic-enrichment-posture.md) for the
no-provider invariant, provider-profile gate, source-policy gate, and security
posture.

## Hosted Component Trust

| Variable | Default | Read by | Purpose |
| --- | --- | --- | --- |
| `ESHU_COMPONENT_TRUST_MODE` | `disabled` | API, MCP, workflow coordinator, component extension collector | Trust mode for hosted component activations and read-only diagnostics. `allowlist` or `strict` is required before enabled component instances can become claim-capable. |
| `ESHU_COMPONENT_ALLOW_IDS` | unset | API, MCP, workflow coordinator, component extension collector | Comma-separated component IDs allowed for hosted activation and reported diagnostics. |
| `ESHU_COMPONENT_ALLOW_PUBLISHERS` | unset | API, MCP, workflow coordinator, component extension collector | Comma-separated publisher identities allowed for hosted activation and reported diagnostics. |
| `ESHU_COMPONENT_REVOKE_IDS` | unset | API, MCP, workflow coordinator, component extension collector | Comma-separated component IDs blocked from receiving new hosted claims and reported diagnostics. |
| `ESHU_COMPONENT_REVOKE_PUBLISHERS` | unset | API, MCP, workflow coordinator, component extension collector | Comma-separated publishers blocked from receiving new hosted claims and reported diagnostics. |
| `ESHU_COMPONENT_CORE_VERSION` | build version | API, MCP, workflow coordinator, component extension collector | Optional core version override used for component compatibility checks and reported diagnostics. |
| `ESHU_COMPONENT_PROVENANCE_CERTIFICATE_IDENTITY` | unset | API, MCP, workflow coordinator, component extension collector | Expected Sigstore certificate identity when `ESHU_COMPONENT_TRUST_MODE=strict`. |
| `ESHU_COMPONENT_PROVENANCE_OIDC_ISSUER` | unset | API, MCP, workflow coordinator, component extension collector | Expected Sigstore OIDC issuer when `ESHU_COMPONENT_TRUST_MODE=strict`. |
| `ESHU_COMPONENT_PROVENANCE_PREDICATE_TYPE` | `slsaprovenance1` | API, MCP, workflow coordinator, component extension collector | Supported Cosign attestation predicate type for strict component trust. |
| `ESHU_COMPONENT_COSIGN_BINARY` | `cosign` | API, MCP, workflow coordinator, component extension collector | Optional Cosign binary override for strict component trust. Do not pass registry tokens through Eshu env; use Cosign's own registry auth mechanisms. |
| `ESHU_COMPONENT_COLLECTOR_INSTANCE_ID` | unset | component extension collector | Optional selector when more than one trusted claim-capable component activation exists. |
| `ESHU_COMPONENT_COLLECTOR_OWNER_ID` | `HOSTNAME`, then `collector-component-extension` | component extension collector | Claim owner label for the process-backed component extension worker. |
| `ESHU_COMPONENT_COLLECTOR_POLL_INTERVAL` | `1s` | component extension collector | Idle poll interval between claim attempts. |
| `ESHU_COMPONENT_COLLECTOR_CLAIM_LEASE_TTL` | workflow default | component extension collector | Lease TTL for claimed component work items. |
| `ESHU_COMPONENT_COLLECTOR_HEARTBEAT_INTERVAL` | workflow default | component extension collector | Heartbeat interval for claimed component work items; must be less than the lease TTL. |
| `ESHU_COMPONENT_COLLECTOR_SCOPE_KIND` | `component` | component extension collector | Fallback SDK claim scope kind used when the activation config has no `host.scope.kind`. |

The coordinator skips revoked, incompatible, disabled, or untrusted component
activations before reconciling collector instances. After trust creates an
activation, the workflow coordinator still requires
`ESHU_HOSTED_EXTENSION_EGRESS_POLICY_JSON` before component-extension work is
planned; missing policy denies extension claims, restricted mode needs a
matching component allow rule, deny rules win, and broad mode is an explicit
opt-in. The process-backed component extension collector applies the same
component trust policy before claiming work. The runtime stores component ID,
version, publisher, manifest digest, runtime protocol, adapter, and a stable
config handle only; operator config paths and credential values stay in the
component registry or runtime environment. When an activation config contains a
`host` block, the coordinator copies only `sourceSystem`, `scope.id`, and
`scope.kind` into workflow rows, and the worker uses `host.scope.kind` for the
SDK claim.

API and MCP read these variables for diagnostics only. They expose registry
state, trust-mode booleans, and stable activation config handles, but never raw
manifest paths, activation config paths, credentials, or community-index
membership as trust.

## Local Installer

| Variable | Default | Read by | Purpose |
| --- | --- | --- | --- |
| `ESHU_VERSION` | `dev` | `scripts/install-local-binaries.sh` | Version string injected into locally built Eshu binaries. |
| `ESHU_LOCAL_OWNER_BUILD_TAGS` | `nolocalllm` | `scripts/install-local-binaries.sh` | Go build tags for the local owner `eshu` binary. The default embeds NornicDB without optional local-LLM pieces. |

## Authentication And Remote CLI

| Variable | Default | Read by | Purpose |
| --- | --- | --- | --- |
| `ESHU_API_KEY` | unset, or generated/persisted when allowed | API, MCP, CLI | Bearer token for local/API calls. |
| `ESHU_AUTO_GENERATE_API_KEY` | `false` | API/MCP runtime key resolver | Allows local runtimes to generate and persist an API key under `ESHU_HOME`. |
| `ESHU_SERVICE_URL` | unset | CLI | Default remote Eshu API base URL. |
| `ESHU_SERVICE_PROFILE` | unset | CLI | Selects `ESHU_SERVICE_URL_<PROFILE>` and `ESHU_API_KEY_<PROFILE>`. |
| `ESHU_SERVICE_URL_<PROFILE>` | unset | CLI | Profile-specific service URL. |
| `ESHU_API_KEY_<PROFILE>` | unset | CLI | Profile-specific API key. |
| `ESHU_MCP_URL` | unset | MCP client setup examples | Deployed MCP HTTP endpoint used by documented client setup commands. |
| `ESHU_MCP_TOKEN` | unset | MCP client setup examples | Deployed MCP bearer token used by documented client setup commands. |
| `ESHU_REMOTE_TIMEOUT_SECONDS` | `30` | CLI HTTP client | Timeout for remote CLI requests. |

## Postgres

| Variable | Default | Read by | Purpose |
| --- | --- | --- | --- |
| `ESHU_FACT_STORE_DSN` | unset | Go runtimes | Primary DSN for fact store and queues. |
| `ESHU_CONTENT_STORE_DSN` | unset | API, MCP, ingester, reducer | DSN for content store and query surfaces. |
| `ESHU_POSTGRES_DSN` | unset | Go runtimes, CLI doctor | Backward-compatible Postgres DSN fallback. Prefer explicit fact/content DSNs in split-service deployments. |
| `ESHU_POSTGRES_MAX_OPEN_CONNS` | `30` | Go runtimes | Maximum open Postgres connections per process. |
| `ESHU_POSTGRES_MAX_IDLE_CONNS` | `10` | Go runtimes | Maximum idle Postgres connections. |
| `ESHU_POSTGRES_CONN_MAX_LIFETIME` | `30m` | Go runtimes | Maximum lifetime of one Postgres connection. |
| `ESHU_POSTGRES_CONN_MAX_IDLE_TIME` | `10m` | Go runtimes | Maximum idle lifetime of one Postgres connection. |
| `ESHU_POSTGRES_PING_TIMEOUT` | `10s` | Go runtimes | Startup ping timeout. |
| `ESHU_CONTENT_ENTITY_BATCH_SIZE` | `300` | bootstrap-index, ingester/projector, projector | Rows per `content_entities` upsert statement. Values must be `1..4000`. |
| `ESHU_CONTENT_WRITER_BATCH_CONCURRENCY` | runtime default | Postgres content writer | Concurrent Postgres content-write batches. Keep within database connection headroom. |
| `ESHU_LOCAL_AUTHORITATIVE_DEFER_CONTENT_SEARCH_INDEXES` | unset / `false` | `eshu graph start` local service | Defers expensive content trigram search indexes during local-authoritative bulk load, then restores them after drain. |

## Graph Driver

| Variable | Default | Read by | Purpose |
| --- | --- | --- | --- |
| `ESHU_NEO4J_URI`, `NEO4J_URI` | unset | API, MCP, ingester, reducer, CLI doctor | Bolt URI for NornicDB or Neo4j. Eshu-prefixed value wins. |
| `ESHU_NEO4J_USERNAME`, `NEO4J_USERNAME` | unset | Graph runtimes | Bolt auth username. |
| `ESHU_NEO4J_PASSWORD`, `NEO4J_PASSWORD` | unset | Graph runtimes | Bolt auth password. |
| `ESHU_NEO4J_DATABASE`, `NEO4J_DATABASE` | `nornic` for default stacks | Graph runtimes | Bolt database name. Use `nornic` for default NornicDB stacks and `neo4j` for Neo4j compatibility. |
| `DEFAULT_DATABASE` | `nornic` for default stacks | API graph open path, CLI config | Legacy/default Bolt database name. Prefer `ESHU_NEO4J_DATABASE`. |
| `ESHU_NEO4J_BATCH_SIZE` | `500` | ingester, reducer, projector, bootstrap-index | Generic graph `UNWIND` row batch size. Prefer label/phase-specific NornicDB knobs when available. |
| `ESHU_NEO4J_MAX_CONNECTION_POOL_SIZE` | `100` | Graph runtimes | Max driver connections. |
| `ESHU_NEO4J_MAX_CONNECTION_LIFETIME` | `1h` | Graph runtimes | Max driver connection lifetime. |
| `ESHU_NEO4J_CONNECTION_ACQUISITION_TIMEOUT` | `1m` | Graph runtimes | Time waiting for a driver connection. |
| `ESHU_NEO4J_SOCKET_CONNECT_TIMEOUT` | `5s` | Graph runtimes | Socket connect timeout. |
| `ESHU_NEO4J_VERIFY_TIMEOUT` | `10s` | Graph runtimes | Startup verification timeout. |
| `ESHU_NEO4J_PROFILE_GROUP_STATEMENTS` | `false` | ingester, bootstrap-index | Logs grouped-write statement attempt timing for Neo4j investigations. |
| `ESHU_GRAPH_SCHEMA_STATEMENT_TIMEOUT` | `2m` | `eshu-bootstrap-data-plane`, bootstrap-index direct marker-missing startup | Per-statement client deadline for graph DDL during schema bootstrap. |
| `ESHU_GRAPH_SCHEMA_ADOPT_EXISTING` | unset: opportunistic for NornicDB, disabled for Neo4j | `eshu-bootstrap-data-plane` | Controls marker-missing graph schema adoption. Truthy values require adoption; false values disable adoption. |

Graph-writing runtimes also read the latest Postgres graph-schema marker at
startup. They do not have a separate environment switch: run
`eshu-bootstrap-data-plane` after changing graph schema so the marker records
the active fingerprint and any explicitly compatible older writer fingerprints.

## Telemetry And Memory

| Variable | Default | Read by | Purpose |
| --- | --- | --- | --- |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | unset | telemetry bootstrap | Enables OTLP traces and metrics. |
| `OTEL_EXPORTER_OTLP_PROTOCOL` | deployment/overlay set | OTEL SDK | OTLP transport protocol, normally `grpc` in local telemetry overlay. |
| `OTEL_EXPORTER_OTLP_INSECURE` | deployment/overlay set | OTEL SDK | Allows insecure OTLP transport for local Compose telemetry. |
| `OTEL_EXPORTER_OTLP_HEADERS` | unset | Helm/OTEL SDK | Extra OTLP headers, usually for deployed collector auth. |
| `OTEL_TRACES_EXPORTER` | deployment/overlay set | OTEL SDK | Trace exporter, normally `otlp`. |
| `OTEL_METRICS_EXPORTER` | deployment/overlay set | OTEL SDK | Metrics exporter, normally `otlp`. |
| `OTEL_LOGS_EXPORTER` | `none` in telemetry overlay | OTEL SDK | Keeps logs on JSON stderr unless the deployment explicitly supports OTEL logs. |
| `OTEL_SERVICE_NAME` | binary/service name | OTEL SDK | Overrides the `service.name` resource attribute. |
| `OTEL_METRIC_EXPORT_INTERVAL` | OTEL SDK default unless Helm sets it | Helm/OTEL SDK | Metrics export cadence. |
| `OTEL_RESOURCE_ATTRIBUTES` | unset unless Helm sets it | Helm/OTEL SDK | Extra resource attributes for deployed telemetry. |
| `OTEL_PYTHON_FASTAPI_EXCLUDED_URLS` | unset unless Helm sets it | Helm templates | Legacy Python-service exclusion wiring retained in Helm helpers. |
| `GOMEMLIMIT` | Go runtime default or cgroup-derived 70% when configured by Eshu | Go runtime | Soft heap target. |
| `GODEBUG` | unset | Go runtime / Eshu memlimit setup | Go runtime debug flags; Eshu may preserve/add memory-limit settings. |
| `ESHU_PROMETHEUS_METRICS_ENABLED` | `true` in Compose services | deployment/Compose | Enables service Prometheus scrape path. |
| `ESHU_PROMETHEUS_METRICS_HOST` | unset unless Helm sets it | Helm templates | Metrics bind host used by deployment templates. |
| `ESHU_PROMETHEUS_METRICS_PORT` | `9464` in Compose services | deployment/Compose | In-container metrics port used by services. |

## Terraform Schema

| Variable | Default | Read by | Purpose |
| --- | --- | --- | --- |
| `ESHU_TERRAFORM_SCHEMA_DIR` | packaged/default schema dir | Terraform schema loader | Overrides Terraform provider schema directory for local schema development or newly generated provider schemas. |
