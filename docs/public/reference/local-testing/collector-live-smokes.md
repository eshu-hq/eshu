# Collector Live Smokes

Use these only for maintainer/operator validation against real external
systems. They are opt-in and must not run in default CI. Keep registry hosts,
account IDs, repository names, usernames, and tokens in local shell config only.

For fixture-backed collector checks, prefer
[Verification gates](verification-gates.md). For hosted all-collector proof, use
[Remote collector E2E](remote-collector-e2e.md).

Standalone live smokes prove the read-only provider fetch
and source-fact envelope path only. They do not prove hosted Compose lifecycle,
workflow claim handoff, container health, or queue-zero. Provider errors such
as PagerDuty `auth_denied` with the current private token are credential proof
failures unless the same target fails with a valid token; keep that evidence
separate from remote Compose service coverage. (The PagerDuty, Jira, and
Confluence standalone smokes are deleted with the in-tree packages; see the
PagerDuty, Jira, and Confluence sections.)

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

PagerDuty collection runs from the extracted satellite
(`eshu-hq/eshu-collector-pagerduty`): the in-tree
`go/internal/collector/pagerduty` package and its
`TestLivePagerDutyIncidentOrConfigEvidence` smoke are deleted, so there is no
in-tree `go test` smoke for PagerDuty. Keep tokens, account IDs, service
IDs, and routing keys in local shell config only.

Run live PagerDuty proof through the satellite release image with the same
target shape (account, credentials passed by `token_env` name reference,
bounded incident/log/change limits, incident lookback, optional service
allowlist, bounded config validation). The satellite's own CI and the QA
activation cutover record (eshu-hq/eshu#8863) carry the bounded-window fetch,
`incident.*`/`change.*` emission, and no-credential-material proof. There is
no remaining in-tree `go test` smoke for PagerDuty.

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
  -f "${ESHU_DEPLOY_CHECKOUT}/compose/docker-compose.remote-e2e.yaml" \
  -f "${ESHU_DEPLOY_CHECKOUT}/compose/docker-compose.remote-e2e.observability.yaml" \
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
  -f "${ESHU_DEPLOY_CHECKOUT}/compose/docker-compose.remote-e2e.yaml" \
  -f "${ESHU_DEPLOY_CHECKOUT}/compose/docker-compose.remote-e2e.observability.yaml" \
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

OCI registry collection runs from the extracted satellite
(`eshu-hq/eshu-collector-oci-registry`): the in-tree
`go/internal/collector/registry/oci` provider packages and their
`TestLive*` smokes are deleted, so there is no in-tree `go test` smoke for
JFrog, ECR, Docker Hub, GHCR, Harbor, Google Artifact Registry, or Azure
Container Registry. Keep registry URLs, repository names, and credentials
in local shell config only.

Run live OCI provider proof through the satellite release image with the
same target shape (provider, registry, repository, region and registry ID
for ECR, repository key for Artifactory, credentials passed by
`username_env`/`password_env`/`bearer_token_env` name reference). The
satellite's own CI carries the bounded fetch, `oci_registry.*` emission,
and no-credential-material proof. The satellite emits no spans or metrics
of its own, so resource and health evidence comes from the
component-extension host. There is no remaining in-tree `go test` smoke
for OCI providers.

## JFrog Package Feed

Package-registry collection runs from the extracted satellite
(`eshu-hq/eshu-collector-package-registry`): the in-tree
`go/internal/collector/registry/package` package and its
`TestLiveJFrogPackageFeed` smoke are deleted, so there is no in-tree
`go test` smoke for the JFrog package feed. Keep metadata URLs, package
coordinates, and credentials in local shell config only.

Run live package-feed proof through the satellite release image with the
same target shape (metadata URL, ecosystem, name, namespace, registry,
credentials passed by name reference). The satellite's own CI carries the
bounded fetch, `package_registry.*` emission, and no-credential-material
proof. There is no remaining in-tree `go test` smoke for the package feed.
