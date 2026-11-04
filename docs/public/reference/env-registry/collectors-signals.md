# Environment Variables: Observability, delivery, and security collectors

<!-- Generated from go/internal/envregistry. Do not edit by hand; regenerate with `bash scripts/generate-env-registry-doc.sh`. -->

Grafana, Prometheus/Mimir, PagerDuty, CI/CD run, security alert, SBOM, and vulnerability collectors. Jira now collects from its extracted satellite (eshu-hq/eshu-collector-jira). Tempo now collects from its extracted satellite (eshu-hq/eshu-collector-tempo). Loki now collects from its extracted satellite (eshu-hq/eshu-collector-loki). This page is part of the [Environment Variable Reference](../env-registry.md), which lists every page. Run `eshu config validate` to check the current environment against the registry.

## collector-cicd-run

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_CICD_RUN_CLAIM_LEASE_TTL` | duration | `60s` | Workflow claim lease TTL. |
| `ESHU_CICD_RUN_COLLECTOR_INSTANCE_ID` | string | — | Instance ID selecting this collector instance from ESHU_COLLECTOR_INSTANCES_JSON. |
| `ESHU_CICD_RUN_COLLECTOR_OWNER_ID` | string | — | Lease owner identifier; defaults to the hostname. |
| `ESHU_CICD_RUN_HEARTBEAT_INTERVAL` | duration | `20s` | Claim heartbeat interval; must be less than the lease TTL. |
| `ESHU_CICD_RUN_POLL_INTERVAL` | duration | `1s` | Poll interval for discovering targets. |

## collector-document-import

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_DOC_IMPORT_ARTIFACT_DIR` | string | — | Absolute directory of reviewed image files to drain. Required when enabled. |
| `ESHU_DOC_IMPORT_ENABLED` | bool | `false` | Opts in to the one-shot reviewed-image OCR drain; anything but true exits 0 with zero commits. |
| `ESHU_DOC_IMPORT_MAX_ARTIFACTS` | int | `1024` | Per-run artifact bound; must stay positive and cannot be disabled. |
| `ESHU_DOC_IMPORT_SCOPE_PREFIX` | string | `document-import-ocr` | Scope ID prefix for drained generations. |
| `ESHU_DOC_IMPORT_SOURCE_SYSTEM` | string | `document_import` | Source system recorded on drained scopes. |
| `ESHU_DOC_IMPORT_TESSERACT_BINARY_PATH` | string | — | Absolute path of the pinned tesseract executable, never resolved via PATH. Required when enabled. |
| `ESHU_DOC_IMPORT_TESSERACT_BINARY_SHA256` | string | — | Pinned hex SHA-256 of the tesseract executable. Required when enabled. |
| `ESHU_DOC_IMPORT_TESSERACT_ENGINE_VERSION` | string | — | Reviewed tesseract release the binary must report, e.g. 5.5.0. Required when enabled. |
| `ESHU_DOC_IMPORT_TESSERACT_LIBRARIES` | string | — | Semicolon-separated path=digest pins for the tesseract shared libraries; at least one. Required when enabled. |
| `ESHU_DOC_IMPORT_TESSERACT_MODEL_SHA256` | string | — | Pinned hex SHA-256 of eng.traineddata. Required when enabled. |
| `ESHU_DOC_IMPORT_TESSERACT_TESSDATA_DIR` | string | — | Absolute tessdata directory holding eng.traineddata. Required when enabled. |
| `ESHU_DOC_IMPORT_TIER` | string | — | Deployment tier allowed to inject the engine; the only accepted value is qa (QA tier, #8879). Required when enabled. |

## collector-grafana

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_GRAFANA_COLLECTOR_CLAIM_LEASE_TTL` | duration | `60s` | Workflow claim lease TTL. |
| `ESHU_GRAFANA_COLLECTOR_HEARTBEAT_INTERVAL` | duration | `20s` | Claim heartbeat interval; must be less than the lease TTL. |
| `ESHU_GRAFANA_COLLECTOR_INSTANCE_ID` | string | — | Instance ID selecting this collector instance from ESHU_COLLECTOR_INSTANCES_JSON. |
| `ESHU_GRAFANA_COLLECTOR_OWNER_ID` | string | — | Lease owner identifier; defaults to the hostname. |
| `ESHU_GRAFANA_COLLECTOR_POLL_INTERVAL` | duration | `1s` | Poll interval for discovering targets. |

## collector-media-import

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_MEDIA_IMPORT_ARTIFACT_DIR` | string | — | Absolute directory of reviewed audio files to drain. Required when enabled. |
| `ESHU_MEDIA_IMPORT_ENABLED` | bool | `false` | Opts in to the one-shot reviewed-audio transcript drain; anything but true exits 0 with zero commits. |
| `ESHU_MEDIA_IMPORT_MAX_ARTIFACTS` | int | `1024` | Per-run artifact bound; must stay positive and cannot be disabled. |
| `ESHU_MEDIA_IMPORT_SCOPE_PREFIX` | string | `media-import-transcript` | Scope ID prefix for drained generations. |
| `ESHU_MEDIA_IMPORT_SOURCE_SYSTEM` | string | `media_import` | Source system recorded on drained scopes. |
| `ESHU_MEDIA_IMPORT_TIER` | string | — | Deployment tier allowed to inject the engine; the only accepted value is qa (QA tier, #8879). Required when enabled. |
| `ESHU_MEDIA_IMPORT_WHISPER_BINARY_PATH` | string | — | Absolute path of the pinned whisper-cli executable, never resolved via PATH. Required when enabled. |
| `ESHU_MEDIA_IMPORT_WHISPER_BINARY_SHA256` | string | — | Pinned hex SHA-256 of the whisper-cli executable. Required when enabled. |
| `ESHU_MEDIA_IMPORT_WHISPER_ENGINE_VERSION` | string | — | Reviewed whisper.cpp release the backend records, e.g. v1.9.5. Required when enabled. |
| `ESHU_MEDIA_IMPORT_WHISPER_MODEL_PATH` | string | — | Absolute path of the pinned ggml model file. Required when enabled. |
| `ESHU_MEDIA_IMPORT_WHISPER_MODEL_SHA256` | string | — | Pinned hex SHA-256 of the model file. Required when enabled. |

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

## collector-vulnerability-intelligence

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_VULNERABILITY_INTELLIGENCE_CLAIM_LEASE_TTL` | duration | `60s` | Workflow claim lease TTL. |
| `ESHU_VULNERABILITY_INTELLIGENCE_COLLECTOR_INSTANCE_ID` | string | — | Instance ID selecting this collector instance from ESHU_COLLECTOR_INSTANCES_JSON. |
| `ESHU_VULNERABILITY_INTELLIGENCE_COLLECTOR_OWNER_ID` | string | — | Lease owner identifier; defaults to the hostname. |
| `ESHU_VULNERABILITY_INTELLIGENCE_HEARTBEAT_INTERVAL` | duration | `20s` | Claim heartbeat interval; must be less than the lease TTL. |
| `ESHU_VULNERABILITY_INTELLIGENCE_POLL_INTERVAL` | duration | `1s` | Poll interval for discovering targets. |
