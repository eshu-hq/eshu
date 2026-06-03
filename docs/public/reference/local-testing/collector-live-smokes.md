# Collector Live Smokes

Use these only for maintainer/operator validation against real external
systems. They are opt-in and must not run in default CI. Keep registry hosts,
account IDs, repository names, usernames, and tokens in local shell config only.

For fixture-backed collector checks, prefer
[Verification gates](verification-gates.md). For hosted all-collector proof, use
[Remote collector E2E](remote-collector-e2e.md).

Jira and PagerDuty standalone live smokes prove the read-only provider fetch
and source-fact envelope path only. They do not prove hosted Compose lifecycle,
workflow claim handoff, container health, or queue-zero. Provider errors such
as PagerDuty `auth_denied` with the current private token are credential proof
failures unless the same target fails with a valid token; keep that evidence
separate from remote Compose service coverage.

## Confluence

Use this when testing the Confluence collector against a real Atlassian site.
The collector is read-only against Confluence and writes documentation facts to
the local Postgres content store.

```bash
set -a
source ~/.jira_api_credentials.conf
set +a

export ESHU_CONFLUENCE_BASE_URL="${CONFLUENCE_BASE_URL:-https://example.atlassian.net/wiki}"
export ESHU_CONFLUENCE_EMAIL="${JIRA_EMAIL:?set JIRA_EMAIL}"
export ESHU_CONFLUENCE_API_TOKEN="${JIRA_API_TOKEN:?set JIRA_API_TOKEN}"
export ESHU_CONFLUENCE_SPACE_KEY="${ESHU_CONFLUENCE_SPACE_KEY:-DEV}"
export ESHU_CONFLUENCE_PAGE_LIMIT="${ESHU_CONFLUENCE_PAGE_LIMIT:-25}"
export ESHU_CONFLUENCE_POLL_INTERVAL="${ESHU_CONFLUENCE_POLL_INTERVAL:-5m}"
```

```bash
export ESHU_CONFLUENCE_SPACE_ID="$(
  curl -fsS \
    -u "${ESHU_CONFLUENCE_EMAIL}:${ESHU_CONFLUENCE_API_TOKEN}" \
    "${ESHU_CONFLUENCE_BASE_URL}/api/v2/spaces?keys=${ESHU_CONFLUENCE_SPACE_KEY}&limit=1" |
    jq -r '.results[0].id'
)"

test -n "$ESHU_CONFLUENCE_SPACE_ID"
test "$ESHU_CONFLUENCE_SPACE_ID" != "null"
```

```bash
docker compose up -d postgres

cd go
go run ./cmd/bootstrap-data-plane
go run ./cmd/collector-confluence
```

In another shell:

```bash
curl -fsS http://127.0.0.1:8080/readyz

docker compose exec -T postgres \
  psql postgresql://eshu:change-me@localhost:5432/eshu \
  -c "select fact_kind, count(*) from fact_records where source_system = 'confluence' group by fact_kind order by fact_kind;"
```

Stop the collector with Ctrl-C after the first successful sync unless you are
testing repeated polling. Record page count, visible document count, emitted
section/link counts, wall time, and HTTP GET count.

Fixture-backed metric proof:

```bash
cd go
go test ./internal/collector/confluence \
  -run 'TestSourceRecordsBoundedConfluenceMetrics|TestHTTPClientRecordsBoundedRequestMetrics' \
  -count=1 -v
```

## Jira

Use this when testing the Jira collector against a real Jira Cloud site. The
smoke is read-only and goes through the same claim-backed source path as the
hosted collector, but it does not require a local Postgres workflow queue. Keep
site URLs, project filters, emails, and tokens in local shell config only.

```bash
set -a
source /path/to/local/private/env
set +a

export ESHU_JIRA_LIVE=1
export ESHU_JIRA_BASE_URL="${ESHU_JIRA_BASE_URL:?set the Jira Cloud site URL}"
export ESHU_JIRA_EMAIL="${ESHU_JIRA_EMAIL:-${JIRA_EMAIL:?set JIRA_EMAIL}}"
export ESHU_JIRA_API_TOKEN="${ESHU_JIRA_API_TOKEN:-${JIRA_API_TOKEN:?set JIRA_API_TOKEN}}"
export ESHU_JIRA_JQL="${ESHU_JIRA_JQL:-}"
export ESHU_JIRA_UPDATED_LOOKBACK="${ESHU_JIRA_UPDATED_LOOKBACK:-168h}"
export ESHU_JIRA_ISSUE_LIMIT="${ESHU_JIRA_ISSUE_LIMIT:-1}"
export ESHU_JIRA_METADATA_LIMIT="${ESHU_JIRA_METADATA_LIMIT:-25}"
```

```bash
cd go
go test ./internal/collector/jira -run TestLiveJiraWorkItemEvidence -count=1 -v
```

The smoke fetches a bounded updated window, issue changelogs, remote links, and
bounded metadata definitions. It fails if no `work_item.*` facts are emitted or
if credential material appears in emitted envelopes or source references.

## Vulnerability Intelligence Fixture

This is not a live hosted smoke. It verifies bounded OSV, CISA KEV, FIRST EPSS,
and NVD request shaping plus source snapshot facts, affected package
normalization, risk-signal facts, fixed-version extraction, and URL credential
stripping.

```bash
cd go
go test ./internal/collector/vulnerabilityintelligence -count=1 -v
```

Hosted vulnerability-intelligence validation must add request budgets,
rate-limit telemetry, fact-emission metrics, admin/status output, and deployment
docs before enabling hosted collection.

## PagerDuty

The PagerDuty smoke is read-only. It skips unless `ESHU_PAGERDUTY_LIVE=1` and a
token is available through `ESHU_PAGERDUTY_API_TOKEN` or
`PAGERDUTY_API_TOKEN`; `PAGERDUTY_USER_API_KEY` is accepted for local
maintainer env files. The default run fetches at most one recent incident and up
to two live config resources so it can still prove source facts when the
incident window is empty.

```bash
set -a
source /path/to/local/private/env
set +a

export ESHU_PAGERDUTY_LIVE=1
export ESHU_PAGERDUTY_INCIDENT_LOOKBACK="${ESHU_PAGERDUTY_INCIDENT_LOOKBACK:-24h}"
export ESHU_PAGERDUTY_INCIDENT_LIMIT="${ESHU_PAGERDUTY_INCIDENT_LIMIT:-1}"
export ESHU_PAGERDUTY_LOG_ENTRY_LIMIT="${ESHU_PAGERDUTY_LOG_ENTRY_LIMIT:-1}"
export ESHU_PAGERDUTY_CHANGE_EVENT_LIMIT="${ESHU_PAGERDUTY_CHANGE_EVENT_LIMIT:-1}"
export ESHU_PAGERDUTY_CONFIG_VALIDATION_ENABLED="${ESHU_PAGERDUTY_CONFIG_VALIDATION_ENABLED:-true}"
export ESHU_PAGERDUTY_CONFIG_RESOURCE_LIMIT="${ESHU_PAGERDUTY_CONFIG_RESOURCE_LIMIT:-2}"

cd go
go test ./internal/collector/pagerduty \
  -run TestLivePagerDutyIncidentOrConfigEvidence -count=1 -v
```

Use `ESHU_PAGERDUTY_ALLOWED_SERVICE_IDS` as a comma-separated allowlist when
you need the live config proof to target a known service. Optional
`ESHU_PAGERDUTY_SCOPE_ID`, `ESHU_PAGERDUTY_ACCOUNT_ID`,
`ESHU_PAGERDUTY_SOURCE_URI`, and `ESHU_PAGERDUTY_API_BASE_URL` override the
default synthetic scope and API endpoint. The smoke fails if no facts are
visible or if the token appears in emitted envelopes.

## Grafana-Stack Observability

These smokes are read-only and claim-path based. They construct the same
claimed source used by the hosted collectors, call `NextClaimed`, and drain the
emitted `observability.*` envelopes without requiring a local workflow queue or
Postgres. They skip by default and run only when the matching live flag is set.

Each smoke fails if the collector emits an unexpected fact family, omits the
source-instance fact, has no provider fetch evidence, or retains configured
token or tenant material in emitted envelopes or error paths. The tests do not
fetch raw logs, spans, metric samples, dashboard JSON, query bodies, label
values, tag values, tenant IDs, or provider response bodies.

```bash
set -a
source /path/to/local/private/env
set +a
```

Grafana requires an API token:

```bash
export ESHU_GRAFANA_LIVE=1
export ESHU_GRAFANA_BASE_URL="${ESHU_GRAFANA_BASE_URL:?set Grafana URL}"
export ESHU_GRAFANA_API_TOKEN="${ESHU_GRAFANA_API_TOKEN:?set Grafana token}"
export ESHU_GRAFANA_RESOURCE_LIMIT="${ESHU_GRAFANA_RESOURCE_LIMIT:-5}"

cd go
go test ./internal/collector/grafana \
  -run TestLiveGrafanaObservedMetadataEvidence -count=1 -v
```

Prometheus and Mimir use the same smoke. Set
`ESHU_PROMETHEUS_MIMIR_PROVIDER` to `prometheus` or `mimir`; the default is
`prometheus`.

```bash
export ESHU_PROMETHEUS_MIMIR_LIVE=1
export ESHU_PROMETHEUS_MIMIR_PROVIDER="${ESHU_PROMETHEUS_MIMIR_PROVIDER:-prometheus}"
export ESHU_PROMETHEUS_MIMIR_BASE_URL="${ESHU_PROMETHEUS_MIMIR_BASE_URL:?set Prometheus or Mimir URL}"
export ESHU_PROMETHEUS_MIMIR_API_TOKEN="${ESHU_PROMETHEUS_MIMIR_API_TOKEN:-}"
export ESHU_PROMETHEUS_MIMIR_TENANT_ID="${ESHU_PROMETHEUS_MIMIR_TENANT_ID:-}"
export ESHU_PROMETHEUS_MIMIR_RESOURCE_LIMIT="${ESHU_PROMETHEUS_MIMIR_RESOURCE_LIMIT:-5}"

cd go
go test ./internal/collector/prometheusmimir \
  -run TestLivePrometheusMimirObservedMetricEvidence -count=1 -v
```

Loki can run unauthenticated when the target allows metadata reads. Label values
are collected only for names explicitly listed in
`ESHU_LOKI_LABEL_VALUE_NAMES`, and values are stored as counts and hashes within
the configured cardinality limit.

```bash
export ESHU_LOKI_LIVE=1
export ESHU_LOKI_BASE_URL="${ESHU_LOKI_BASE_URL:?set Loki URL}"
export ESHU_LOKI_API_TOKEN="${ESHU_LOKI_API_TOKEN:-}"
export ESHU_LOKI_TENANT_ID="${ESHU_LOKI_TENANT_ID:-}"
export ESHU_LOKI_LABEL_VALUE_NAMES="${ESHU_LOKI_LABEL_VALUE_NAMES:-}"
export ESHU_LOKI_SERIES_MATCHERS="${ESHU_LOKI_SERIES_MATCHERS:-}"
export ESHU_LOKI_RESOURCE_LIMIT="${ESHU_LOKI_RESOURCE_LIMIT:-5}"
export ESHU_LOKI_MAX_LABEL_VALUES_PER_LABEL="${ESHU_LOKI_MAX_LABEL_VALUES_PER_LABEL:-5}"

cd go
go test ./internal/collector/loki \
  -run TestLiveLokiObservedLogSignalEvidence -count=1 -v
```

Tempo can run unauthenticated when the target allows metadata reads. Tag values
are collected only for names explicitly listed in
`ESHU_TEMPO_TAG_VALUE_NAMES`, and values are stored as counts and hashes within
the configured cardinality limit.

```bash
export ESHU_TEMPO_LIVE=1
export ESHU_TEMPO_BASE_URL="${ESHU_TEMPO_BASE_URL:?set Tempo URL}"
export ESHU_TEMPO_API_TOKEN="${ESHU_TEMPO_API_TOKEN:-}"
export ESHU_TEMPO_TENANT_ID="${ESHU_TEMPO_TENANT_ID:-}"
export ESHU_TEMPO_TAG_VALUE_NAMES="${ESHU_TEMPO_TAG_VALUE_NAMES:-}"
export ESHU_TEMPO_LOOKBACK="${ESHU_TEMPO_LOOKBACK:-1h}"
export ESHU_TEMPO_RESOURCE_LIMIT="${ESHU_TEMPO_RESOURCE_LIMIT:-5}"
export ESHU_TEMPO_MAX_TAG_VALUES_PER_TAG="${ESHU_TEMPO_MAX_TAG_VALUES_PER_TAG:-5}"
export ESHU_TEMPO_FRESHNESS_PROBE="${ESHU_TEMPO_FRESHNESS_PROBE:-false}"

cd go
go test ./internal/collector/tempo \
  -run TestLiveTempoObservedTraceSignalEvidence -count=1 -v
```

To run all four against a prepared environment:

```bash
cd go
go test ./internal/collector/grafana \
  ./internal/collector/prometheusmimir \
  ./internal/collector/loki \
  ./internal/collector/tempo \
  -run 'TestLive.*Observed.*Evidence' -count=1 -v
```

## OCI Registry Smokes

All smokes are read-only and skip unless their live flag is set.

| Provider | Required live flag | Command |
| --- | --- | --- |
| JFrog OCI | `ESHU_JFROG_OCI_LIVE=1` | `cd go && go test ./internal/collector/ociregistry/jfrog -run TestLiveJFrog -count=1 -v` |
| ECR | `ESHU_ECR_OCI_LIVE=1` | `cd go && go test ./internal/collector/ociregistry/ecr -run TestLiveECR -count=1 -v` |
| Docker Hub | `ESHU_DOCKERHUB_OCI_LIVE=1` | `cd go && go test ./internal/collector/ociregistry/dockerhub -run TestLiveDockerHub -count=1 -v` |
| GHCR | `ESHU_GHCR_OCI_LIVE=1` | `cd go && go test ./internal/collector/ociregistry/ghcr -run TestLiveGHCR -count=1 -v` |
| Harbor | `ESHU_HARBOR_OCI_LIVE=1` | `cd go && go test ./internal/collector/ociregistry/harbor -run TestLiveHarbor -count=1 -v` |
| Google Artifact Registry | `ESHU_GAR_OCI_LIVE=1` | `cd go && go test ./internal/collector/ociregistry/gar -run TestLiveGAR -count=1 -v` |
| Azure Container Registry | `ESHU_ACR_OCI_LIVE=1` | `cd go && go test ./internal/collector/ociregistry/acr -run TestLiveACR -count=1 -v` |

Minimum provider env:

| Provider | Required target env |
| --- | --- |
| JFrog OCI | `ESHU_JFROG_OCI_URL`; tag-list proof also needs `ESHU_JFROG_OCI_IMAGE_REPOSITORY`; Artifactory Docker API proof needs `ESHU_JFROG_OCI_REPOSITORY_KEY`. |
| ECR | `ESHU_ECR_OCI_REGION`, `ESHU_ECR_OCI_REGISTRY_ID`, `ESHU_ECR_OCI_REPOSITORY`; `ESHU_ECR_OCI_REFERENCE` is optional. |
| Docker Hub | `ESHU_DOCKERHUB_OCI_REPOSITORY`; use username/password for private repositories or rate-limit avoidance. |
| GHCR | `ESHU_GHCR_OCI_REPOSITORY`; use username/password for private or organization packages that deny anonymous pulls. |
| Harbor | `ESHU_HARBOR_OCI_URL`, `ESHU_HARBOR_OCI_REPOSITORY`, username/password when required. |
| Google Artifact Registry | `ESHU_GAR_OCI_REGISTRY_HOST`, `ESHU_GAR_OCI_REPOSITORY`, username/password when required. |
| Azure Container Registry | `ESHU_ACR_OCI_REGISTRY_HOST`, `ESHU_ACR_OCI_REPOSITORY`, username/password when required. |

Use `ESHU_*_OCI_REFERENCE` for reference-specific checks when the provider test
supports it. Use `ESHU_ECR_OCI_REGISTRY_HOST` for nonstandard ECR host shapes.

## JFrog Package Feed

```bash
set -a
source /path/to/local/private/env
set +a

export ESHU_JFROG_PACKAGE_LIVE=1
export ESHU_JFROG_PACKAGE_METADATA_URL="${ESHU_JFROG_PACKAGE_METADATA_URL:?set an Artifactory package metadata wrapper URL}"
export ESHU_JFROG_PACKAGE_ECOSYSTEM="${ESHU_JFROG_PACKAGE_ECOSYSTEM:-npm}"
export ESHU_JFROG_PACKAGE_NAME="${ESHU_JFROG_PACKAGE_NAME:?set the package name in the metadata document}"
export ESHU_JFROG_PACKAGE_NAMESPACE="${ESHU_JFROG_PACKAGE_NAMESPACE:-}"
export ESHU_JFROG_PACKAGE_REGISTRY="${ESHU_JFROG_PACKAGE_REGISTRY:-${JFROG_PACKAGE_REGISTRY:-${JFROG_URL:-${JFROG_BASE_URL:-}}}}"
export ESHU_JFROG_PACKAGE_USERNAME="${ESHU_JFROG_PACKAGE_USERNAME:-${JFROG_USERNAME:-${JFROG_USER:-}}}"
export ESHU_JFROG_PACKAGE_PASSWORD="${ESHU_JFROG_PACKAGE_PASSWORD:-${JFROG_PASSWORD:-}}"
export ESHU_JFROG_PACKAGE_BEARER_TOKEN="${ESHU_JFROG_PACKAGE_BEARER_TOKEN:-${JFROG_ACCESS_TOKEN:-${JFROG_BEARER_TOKEN:-}}}"

cd go
go test ./internal/collector/packageregistry/packageruntime \
  -run TestLiveJFrogPackageFeed -count=1 -v
```

The package smoke strips query strings and fragments from emitted source
references and fails if credential material appears in errors, source refs, or
fact payloads. For Maven, set `ESHU_JFROG_PACKAGE_NAMESPACE` to the package
`groupId`.
