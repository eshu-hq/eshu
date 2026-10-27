# Collector Live Smokes

Use these only for maintainer/operator validation against real external
systems. They are opt-in and must not run in default CI. Keep registry hosts,
account IDs, repository names, usernames, and tokens in local shell config only.

For fixture-backed collector checks, prefer
[Verification gates](verification-gates.md). For hosted all-collector proof, use
[Remote collector E2E](remote-collector-e2e.md).

PagerDuty standalone live smokes prove the read-only provider fetch
and source-fact envelope path only. They do not prove hosted Compose lifecycle,
workflow claim handoff, container health, or queue-zero. Provider errors such
as PagerDuty `auth_denied` with the current private token are credential proof
failures unless the same target fails with a valid token; keep that evidence
separate from remote Compose service coverage. (The Jira and Confluence
standalone smokes are deleted with the in-tree packages; see the Jira and
Confluence sections.)

## Confluence

Confluence collection runs from the extracted satellite
(`eshu-hq/eshu-collector-confluence`): the in-tree
`go/internal/collector/confluence` package and its
`go/cmd/collector-confluence` service are deleted, so there is no in-tree
`go run` or `go test` smoke for Confluence. Keep site URLs, space IDs,
emails, and tokens in local shell config only.

Run live Confluence proof through the satellite release image with the same
target shape (host, credentials passed by `token_env`/`email_env` name
reference, bounded space or page-tree scope, page/total-page limits).
The satellite's own CI and the QA activation cutover record
(eshu-hq/eshu#8855) carry the bounded-space fetch, `documentation_*`
emission, and no-credential-material proof. The satellite emits no metrics
or traces of its own, so resource and health evidence comes from the
component-extension host. There is no remaining in-tree `go test` smoke for
Confluence.

## Jira

Jira collection runs from the extracted satellite
(`eshu-hq/eshu-collector-jira`): the in-tree
`go/internal/collector/jira` package and its
`TestLiveJiraWorkItemEvidence` smoke are deleted, so there is no in-tree
`go test` smoke for Jira. Keep site URLs, project filters, emails, and tokens
in local shell config only.

Run live Jira proof through the satellite release image with the same target
shape (site, credentials passed by `token_env`/`email_env` name reference,
bounded JQL scope, issue/changelog/remote-link limits, updated lookback).
The satellite's own CI and the QA activation cutover record (eshu-hq/eshu#8858)
carry the bounded-window fetch, changelog/remote-link/metadata, `work_item.*`
emission, and no-credential-material proof. There is no remaining in-tree
`go test` smoke for Jira.

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
go test ./internal/collector/observability/grafana \
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
go test ./internal/collector/observability/prometheus \
  -run TestLivePrometheusMimirObservedMetricEvidence -count=1 -v
```

Loki collection runs from the extracted satellite
(`eshu-hq/eshu-collector-loki`): the in-tree
`go/internal/collector/observability/loki` package and its
`TestLiveLokiObservedLogSignalEvidence` smoke are deleted, so there is no
in-tree `go test` smoke for Loki. Label values are still collected only for
names explicitly listed in `label_value_names`, stored as counts and hashes
within the cardinality limit, and Loki can run unauthenticated when the target
allows metadata reads.

Run live Loki proof through the hosted `loki` profile in
`docker-compose.remote-e2e.observability.yaml` — the satellite image with the
same target shape, credentials passed by `token_env`/`tenant_id_env` name
reference. Set up the private env file as shown in [Remote Collector Hosted
Profiles](remote-collector-hosted-profiles.md) — the `loki` profile enable
flag, `ESHU_LOKI_BASE_URL`, and the token/tenant `*_ENV` name references
(the `collector-loki-reconcile` service in
`docker-compose.remote-e2e.observability.yaml` lists every variable) — then
render the profile:

```bash
docker compose --env-file path/to/private-remote-e2e.env \
  -f docker-compose.remote-e2e.yaml \
  -f docker-compose.remote-e2e.observability.yaml \
  --profile loki config --services
```

Then start the profile and capture proof with
`scripts/verify_remote_e2e_runtime_state.sh` as shown on that page.

Tempo collection runs from the extracted satellite
(`eshu-hq/eshu-collector-tempo`): the in-tree
`go/internal/collector/observability/tempo` package and its
`TestLiveTempoObservedTraceSignalEvidence` smoke are deleted, so there is no
in-tree `go test` smoke for Tempo. Tag values are still collected only for
allowlisted `tag_value_names`, stored as counts and hashes within the
cardinality limit, and Tempo can run unauthenticated when the target allows
metadata reads.

Run live Tempo proof through the hosted `tempo` profile in
`docker-compose.remote-e2e.observability.yaml` — the satellite image with the
same target shape, credentials passed by `token_env`/`tenant_id_env` name
reference. Set up the private env file as shown in [Remote Collector Hosted
Profiles](remote-collector-hosted-profiles.md) — the `tempo` profile enable
flag, `ESHU_TEMPO_BASE_URL`, and the token/tenant `*_ENV` name references
(the `collector-tempo` service in `docker-compose.remote-e2e.observability.yaml`
lists every variable) — then render the profile:

```bash
docker compose --env-file path/to/private-remote-e2e.env \
  -f docker-compose.remote-e2e.yaml \
  -f docker-compose.remote-e2e.observability.yaml \
  --profile tempo config --services
```

Then start the profile and capture proof with
`scripts/verify_remote_e2e_runtime_state.sh` as shown on that page.

To run both remaining in-tree smokes against a prepared environment:

```bash
cd go
go test ./internal/collector/observability/grafana \
  ./internal/collector/observability/prometheus \
  -run 'TestLive.*Observed.*Evidence' -count=1 -v
```

Tempo and Loki run separately from the satellite profiles above; they are no longer part
of this combined command.

## OCI Registry Smokes

All smokes are read-only and skip unless their live flag is set.

| Provider | Required live flag | Command |
| --- | --- | --- |
| JFrog OCI | `ESHU_JFROG_OCI_LIVE=1` | `cd go && go test ./internal/collector/registry/oci/jfrog -run TestLiveJFrog -count=1 -v` |
| ECR | `ESHU_ECR_OCI_LIVE=1` | `cd go && go test ./internal/collector/registry/oci/ecr -run TestLiveECR -count=1 -v` |
| Docker Hub | `ESHU_DOCKERHUB_OCI_LIVE=1` | `cd go && go test ./internal/collector/registry/oci/dockerhub -run TestLiveDockerHub -count=1 -v` |
| GHCR | `ESHU_GHCR_OCI_LIVE=1` | `cd go && go test ./internal/collector/registry/oci/ghcr -run TestLiveGHCR -count=1 -v` |
| Harbor | `ESHU_HARBOR_OCI_LIVE=1` | `cd go && go test ./internal/collector/registry/oci/harbor -run TestLiveHarbor -count=1 -v` |
| Google Artifact Registry | `ESHU_GAR_OCI_LIVE=1` | `cd go && go test ./internal/collector/registry/oci/gar -run TestLiveGAR -count=1 -v` |
| Azure Container Registry | `ESHU_ACR_OCI_LIVE=1` | `cd go && go test ./internal/collector/registry/oci/acr -run TestLiveACR -count=1 -v` |

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
go test ./internal/collector/registry/package/runtime \
  -run TestLiveJFrogPackageFeed -count=1 -v
```

The package smoke strips query strings and fragments from emitted source
references and fails if credential material appears in errors, source refs, or
fact payloads. For Maven, set `ESHU_JFROG_PACKAGE_NAMESPACE` to the package
`groupId`.
