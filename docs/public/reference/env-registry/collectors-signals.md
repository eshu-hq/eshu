# Environment Variables: Observability, delivery, and security collectors

<!-- Generated from go/internal/envregistry. Do not edit by hand; regenerate with `bash scripts/generate-env-registry-doc.sh`. -->

Grafana, Loki, Prometheus/Mimir, Tempo, PagerDuty, CI/CD run, security alert, SBOM, and vulnerability collectors. Jira now collects from its extracted satellite (eshu-hq/eshu-collector-jira). This page is part of the [Environment Variable Reference](../env-registry.md), which lists every page. Run `eshu config validate` to check the current environment against the registry.

## collector-cicd-run

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_CICD_RUN_CLAIM_LEASE_TTL` | duration | `60s` | Workflow claim lease TTL. |
| `ESHU_CICD_RUN_COLLECTOR_INSTANCE_ID` | string | — | Instance ID selecting this collector instance from ESHU_COLLECTOR_INSTANCES_JSON. |
| `ESHU_CICD_RUN_COLLECTOR_OWNER_ID` | string | — | Lease owner identifier; defaults to the hostname. |
| `ESHU_CICD_RUN_HEARTBEAT_INTERVAL` | duration | `20s` | Claim heartbeat interval; must be less than the lease TTL. |
| `ESHU_CICD_RUN_POLL_INTERVAL` | duration | `1s` | Poll interval for discovering targets. |

## collector-grafana

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_GRAFANA_COLLECTOR_CLAIM_LEASE_TTL` | duration | `60s` | Workflow claim lease TTL. |
| `ESHU_GRAFANA_COLLECTOR_HEARTBEAT_INTERVAL` | duration | `20s` | Claim heartbeat interval; must be less than the lease TTL. |
| `ESHU_GRAFANA_COLLECTOR_INSTANCE_ID` | string | — | Instance ID selecting this collector instance from ESHU_COLLECTOR_INSTANCES_JSON. |
| `ESHU_GRAFANA_COLLECTOR_OWNER_ID` | string | — | Lease owner identifier; defaults to the hostname. |
| `ESHU_GRAFANA_COLLECTOR_POLL_INTERVAL` | duration | `1s` | Poll interval for discovering targets. |

## collector-loki

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_LOKI_COLLECTOR_CLAIM_LEASE_TTL` | duration | `60s` | Workflow claim lease TTL. |
| `ESHU_LOKI_COLLECTOR_HEARTBEAT_INTERVAL` | duration | `20s` | Claim heartbeat interval; must be less than the lease TTL. |
| `ESHU_LOKI_COLLECTOR_INSTANCE_ID` | string | — | Instance ID selecting this collector instance from ESHU_COLLECTOR_INSTANCES_JSON. |
| `ESHU_LOKI_COLLECTOR_OWNER_ID` | string | — | Lease owner identifier; defaults to the hostname. |
| `ESHU_LOKI_COLLECTOR_POLL_INTERVAL` | duration | `1s` | Poll interval for discovering targets. |

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

## collector-vulnerability-intelligence

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_VULNERABILITY_INTELLIGENCE_CLAIM_LEASE_TTL` | duration | `60s` | Workflow claim lease TTL. |
| `ESHU_VULNERABILITY_INTELLIGENCE_COLLECTOR_INSTANCE_ID` | string | — | Instance ID selecting this collector instance from ESHU_COLLECTOR_INSTANCES_JSON. |
| `ESHU_VULNERABILITY_INTELLIGENCE_COLLECTOR_OWNER_ID` | string | — | Lease owner identifier; defaults to the hostname. |
| `ESHU_VULNERABILITY_INTELLIGENCE_HEARTBEAT_INTERVAL` | duration | `20s` | Claim heartbeat interval; must be less than the lease TTL. |
| `ESHU_VULNERABILITY_INTELLIGENCE_POLL_INTERVAL` | duration | `1s` | Poll interval for discovering targets. |
