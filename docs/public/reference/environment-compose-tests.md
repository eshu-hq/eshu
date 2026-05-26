# Compose And Test Environment

This page covers Docker Compose, remote E2E, verifier, proof-run, and live-smoke
environment variables. These are mostly local or CI surfaces, not production
tuning knobs.

## Local Compose MCP Helper

These variables are read by `scripts/sync_local_compose_mcp.sh`.

| Variable | Default | Purpose |
| --- | --- | --- |
| `ESHU_MCP_CONFIG_FILE` | repo-local `.mcp.json` | MCP client config file to update with the local Compose server entry. |
| `ESHU_LOCAL_MCP_SERVER_NAME` | `eshu-local-compose` | Server entry name written into MCP config. |
| `ESHU_LOCAL_MCP_URL` | discovered from Compose `mcp-server:8080` | Overrides the local MCP JSON-RPC URL. |
| `ESHU_LOCAL_MCP_TOKEN` | discovered from the Compose MCP runtime | Overrides the bearer token written into local MCP config. |
| `ESHU_LOCAL_API_URL` | discovered from Compose `eshu:8080` | Overrides the API URL used for the index-status probe. |
| `ESHU_SKIP_PROBES` | `false` | Skips MCP health, MCP tools/list, and API index-status probes. |

## Local Compose Ports And Storage

| Variable | Default | Purpose |
| --- | --- | --- |
| `ESHU_FILESYSTEM_HOST_ROOT` | `./tests/fixtures/ecosystems` | Host repo root mounted into Compose fixtures path. |
| `ESHU_ESHUIGNORE_PATH` | `/dev/null` in Compose | Optional host `.eshuignore` mounted into bootstrap/ingester containers. |
| `ESHU_HTTP_PORT` | `8080` | Host port for Eshu API. |
| `ESHU_MCP_PORT` | `8081` | Host port for MCP HTTP service. |
| `ESHU_API_METRICS_PORT` | `19464` | Host metrics port for API. |
| `ESHU_INGESTER_METRICS_PORT` | `19465` | Host metrics port for ingester. |
| `ESHU_RESOLUTION_ENGINE_METRICS_PORT` | `19466` | Host metrics port for resolution engine. |
| `ESHU_BOOTSTRAP_METRICS_PORT` | `19467` | Host metrics port for bootstrap-index. |
| `ESHU_MCP_METRICS_PORT` | `19468` | Host metrics port for MCP server. |
| `ESHU_WORKFLOW_COORDINATOR_HTTP_PORT` | `18082` | Host HTTP port for workflow coordinator. |
| `ESHU_WORKFLOW_COORDINATOR_METRICS_PORT` | `19469` | Host metrics port for workflow coordinator. |
| `ESHU_WEBHOOK_LISTENER_HTTP_PORT` | `18083` | Host HTTP/metrics port for optional webhook listener profile. |
| `NEO4J_HTTP_PORT` | `7474` | Host Neo4j/NornicDB HTTP port in examples. |
| `NEO4J_BOLT_PORT` | `7687` | Host Neo4j/NornicDB Bolt port in examples. |
| `ESHU_POSTGRES_PORT` | `15432` | Host Postgres port. |
| `ESHU_POSTGRES_PASSWORD` | `change-me` | Compose Postgres password used by generated DSNs. |

## Compose Database And Backend Settings

| Variable | Default | Purpose |
| --- | --- | --- |
| `NEO4J_AUTH` | `neo4j/${ESHU_NEO4J_PASSWORD:-change-me}` in compose | Neo4j container auth string. |
| `NEO4J_AUTH_ENABLED` | `true` in legacy compose template | Enables Neo4j auth in the template variant. |
| `NEO4J_PLUGINS` | `[]` in Compose | Neo4j plugin list. |
| `ESHU_NEO4J_HEAP_INITIAL_SIZE` | `512m` in Compose | Neo4j initial heap size. |
| `ESHU_NEO4J_HEAP_MAX_SIZE` | `512m` in Compose | Neo4j max heap size. |
| `ESHU_NEO4J_PAGECACHE_SIZE` | `512m` in docs/examples | Neo4j page cache budget. |
| `ESHU_PG_SHARED_BUFFERS` | `4GB` in Compose | Postgres shared buffers. |
| `ESHU_PG_WORK_MEM` | `16MB` in Compose | Per-operation Postgres work memory. |
| `ESHU_PG_MAINTENANCE_WORK_MEM` | `512MB` in Compose | Postgres maintenance operation memory. |
| `ESHU_PG_MAX_WAL_SIZE` | `8GB` in Compose | WAL size before checkpoint pressure. |
| `ESHU_PG_WAL_BUFFERS` | `64MB` in Compose | WAL buffer budget. |
| `ESHU_PG_EFFECTIVE_CACHE_SIZE` | `32GB` in Compose | Planner estimate of OS cache. |
| `ESHU_PG_SYNCHRONOUS_COMMIT` | `off` in Compose | Local durability/latency trade-off. |
| `ESHU_PG_TOAST_COMPRESSION` | `lz4` in Compose | TOAST compression algorithm. |
| `OTEL_COLLECTOR_OTLP_GRPC_PORT` | `4317` in Compose | Host OTLP gRPC port. |
| `OTEL_COLLECTOR_OTLP_HTTP_PORT` | `4318` in Compose | Host OTLP HTTP port. |
| `OTEL_COLLECTOR_PROMETHEUS_PORT` | `9464` in Compose | Host Prometheus scrape/export port. |

## Remote E2E

| Variable | Default | Purpose |
| --- | --- | --- |
| `ESHU_REMOTE_E2E_PROJECT_NAME` | `eshu-remote-e2e` | Compose project name for remote E2E. |
| `ESHU_REMOTE_E2E_CORPUS_MODE` | `smoke` in Compose and `.env.remote-e2e.example` | Declares `smoke`, `representative`, or `full` corpus mode. |
| `ESHU_REMOTE_E2E_MIN_REPOSITORY_COUNT` | `0` in Compose | Minimum candidate repository-root count accepted by corpus preflight. |
| `ESHU_REMOTE_E2E_MAX_REPOSITORY_COUNT` | unset | Maximum candidate repository-root count accepted by corpus preflight. Representative mode defaults this to `50` when unset. |
| `ESHU_REMOTE_E2E_EXPECTED_REPOSITORY_COUNT` | unset | Exact candidate repository-root count accepted by corpus preflight. |
| `ESHU_REMOTE_E2E_MIN_PACKAGE_COUNT` | `1` in representative mode, otherwise `0` | Minimum package-registry aggregate count accepted by the remote runtime verifier. |
| `ESHU_REMOTE_E2E_MIN_ADVISORY_EVIDENCE_COUNT` | `1` in representative mode, otherwise `0` | Minimum scoped advisory-evidence count accepted by the remote runtime verifier. |
| `ESHU_REMOTE_E2E_MIN_IMPACT_FINDING_COUNT` | `1` in representative mode, otherwise `0` | Minimum supply-chain impact finding count accepted by the remote runtime verifier. |
| `ESHU_REMOTE_E2E_MIN_SECURITY_ALERT_RECONCILIATION_COUNT` | `1` in representative mode, otherwise `0` | Minimum security-alert reconciliation count accepted by the remote runtime verifier. |
| `ESHU_REMOTE_E2E_MIN_SBOM_ATTACHMENT_COUNT` | `1` in representative mode, otherwise `0` | Minimum SBOM attachment aggregate count accepted by the remote runtime verifier. |
| `ESHU_REMOTE_E2E_MIN_CONTAINER_IMAGE_IDENTITY_COUNT` | `1` in representative mode, otherwise `0` | Minimum container-image identity aggregate count accepted by the remote runtime verifier. |
| `ESHU_REMOTE_E2E_ENV_FILE` | unset | Optional Compose env file passed to `docker compose --env-file`. |
| `ESHU_REMOTE_E2E_COMPOSE_FILES` | `docker-compose.remote-e2e.yaml` | Colon-separated Compose file list for `scripts/verify_remote_e2e_runtime_state.sh`. |
| `ESHU_REMOTE_E2E_REQUIRED_SERVICES` | core runtime service list | Core services that must be running and healthy. |
| `ESHU_REMOTE_E2E_COLLECTOR_SERVICES` | hosted collector service list | Collector services that must be running and healthy. |
| `ESHU_REMOTE_E2E_EXTRA_SERVICES` | unset | Additional services required for a profile-specific proof. |
| `ESHU_REMOTE_E2E_API_BASE_URL` | discovered from Compose `eshu:8080` | API base URL for the checkpointed index-status probe. |
| `ESHU_REMOTE_E2E_API_KEY` | discovered from Compose runtime when available | Bearer token for the index-status probe. |
| `ESHU_AWS_E2E_ACCOUNT_ID` | unset | AWS account ID used by remote E2E AWS targets. |
| `ESHU_AWS_E2E_REGION` | unset | AWS region used by remote E2E AWS targets. |
| `ESHU_AWS_E2E_SERVICES` | broad service list in remote E2E Compose | AWS scanner families enabled by the remote E2E proof. |
| `ESHU_TFSTATE_S3_BUCKET` | unset | Remote E2E Terraform-state S3 bucket. |
| `ESHU_TFSTATE_S3_KEY` | unset | Remote E2E Terraform-state object key. |
| `ESHU_TFSTATE_S3_REGION` | unset | Remote E2E Terraform-state S3 region. |

## Collector Metrics Ports In Remote E2E

| Variable | Default | Purpose |
| --- | --- | --- |
| `ESHU_COLLECTOR_TFSTATE_METRICS_PORT` | `19470` in remote E2E | Terraform-state collector host metrics port. |
| `ESHU_COLLECTOR_OCI_REGISTRY_METRICS_PORT` | `19471` in remote E2E | OCI registry collector host metrics port. |
| `ESHU_COLLECTOR_PACKAGE_REGISTRY_METRICS_PORT` | `19472` in remote E2E | Package-registry collector host metrics port. |
| `ESHU_COLLECTOR_AWS_CLOUD_METRICS_PORT` | `19473` in remote E2E | AWS cloud collector host metrics port. |
| `ESHU_COLLECTOR_CONFLUENCE_METRICS_PORT` | `19474` in remote E2E | Confluence collector host metrics port. |
| `ESHU_COLLECTOR_SECURITY_ALERTS_METRICS_PORT` | `19479` in remote E2E | Security-alert collector host metrics port. |
| `ESHU_COLLECTOR_GIT_METRICS_PORT` | verifier-selected | Git collector runtime verifier metrics port. |
| `ESHU_COLLECTOR_TFSTATE_GEN1_METRICS_PORT`, `ESHU_COLLECTOR_TFSTATE_GEN2_METRICS_PORT` | verifier-selected | Terraform-state v25 tier-2 proof metrics ports. |

## Test And Proof Gates

| Variable | Default | Purpose |
| --- | --- | --- |
| `ESHU_KEEP_COMPOSE_STACK` | unset / `false` | Skips automatic `docker compose down -v` cleanup after verifier runs. |
| `ESHU_LOCAL_AUTHORITATIVE_PERF` | unset / `false` | Enables local-authoritative startup/query performance smoke tests. |
| `ESHU_TFSTATE_100MIB_PROOF` | unset / `false` | Enables the 100 MiB Terraform-state streaming proof. |
| `ESHU_TFSTATE_DRIFT_PROOF_OUT` | unset | Optional proof artifact path for Terraform-state drift verification. |
| `ESHU_TFSTATE_DRIFT_V25_PROOF_OUT` | unset | Optional proof artifact path for the v25 tier-2 drift verifier. |

## Live-Smoke Variables

Live-smoke variables are maintainer-only proof inputs. They are documented in
[Collector Live Smokes](local-testing/collector-live-smokes.md), not tuned in
normal runtime operations.

| Collector | Variables |
| --- | --- |
| JFrog OCI | `ESHU_JFROG_OCI_LIVE`, `ESHU_JFROG_OCI_URL`, `ESHU_JFROG_OCI_REPOSITORY_KEY`, `ESHU_JFROG_OCI_IMAGE_REPOSITORY`, `ESHU_JFROG_OCI_REFERENCE`, `ESHU_JFROG_OCI_USERNAME`, `ESHU_JFROG_OCI_PASSWORD`, `ESHU_JFROG_OCI_BEARER_TOKEN` |
| JFrog package registry | `ESHU_JFROG_PACKAGE_LIVE`, `ESHU_JFROG_PACKAGE_METADATA_URL`, `ESHU_JFROG_PACKAGE_ECOSYSTEM`, `ESHU_JFROG_PACKAGE_NAME`, `ESHU_JFROG_PACKAGE_NAMESPACE`, `ESHU_JFROG_PACKAGE_REGISTRY`, `ESHU_JFROG_PACKAGE_USERNAME`, `ESHU_JFROG_PACKAGE_PASSWORD`, `ESHU_JFROG_PACKAGE_BEARER_TOKEN` |
| AWS ECR OCI | `ESHU_ECR_OCI_LIVE`, `ESHU_ECR_OCI_REGION`, `ESHU_ECR_OCI_REGISTRY_ID`, `ESHU_ECR_OCI_REPOSITORY`, `ESHU_ECR_OCI_REFERENCE`, `ESHU_ECR_OCI_REGISTRY_HOST` |
| Docker Hub OCI | `ESHU_DOCKERHUB_OCI_LIVE`, `ESHU_DOCKERHUB_OCI_REPOSITORY`, `ESHU_DOCKERHUB_OCI_REFERENCE`, `ESHU_DOCKERHUB_OCI_USERNAME`, `ESHU_DOCKERHUB_OCI_PASSWORD` |
| GHCR OCI | `ESHU_GHCR_OCI_LIVE`, `ESHU_GHCR_OCI_REPOSITORY`, `ESHU_GHCR_OCI_REFERENCE`, `ESHU_GHCR_OCI_USERNAME`, `ESHU_GHCR_OCI_PASSWORD` |
| Harbor OCI | `ESHU_HARBOR_OCI_LIVE`, `ESHU_HARBOR_OCI_URL`, `ESHU_HARBOR_OCI_REPOSITORY`, `ESHU_HARBOR_OCI_REFERENCE`, `ESHU_HARBOR_OCI_USERNAME`, `ESHU_HARBOR_OCI_PASSWORD`, `ESHU_HARBOR_OCI_BEARER_TOKEN` |
| Google Artifact Registry OCI | `ESHU_GAR_OCI_LIVE`, `ESHU_GAR_OCI_REGISTRY_HOST`, `ESHU_GAR_OCI_REPOSITORY`, `ESHU_GAR_OCI_REFERENCE`, `ESHU_GAR_OCI_USERNAME`, `ESHU_GAR_OCI_PASSWORD`, `ESHU_GAR_OCI_BEARER_TOKEN` |
| Azure Container Registry OCI | `ESHU_ACR_OCI_LIVE`, `ESHU_ACR_OCI_REGISTRY_HOST`, `ESHU_ACR_OCI_REPOSITORY`, `ESHU_ACR_OCI_REFERENCE`, `ESHU_ACR_OCI_USERNAME`, `ESHU_ACR_OCI_PASSWORD`, `ESHU_ACR_OCI_BEARER_TOKEN` |
