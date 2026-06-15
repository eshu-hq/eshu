# Remote Collector Hosted Profiles

Use this page with
[Remote Collector E2E](remote-collector-e2e.md) when a prerelease proof needs
the hosted Jira, PagerDuty, Grafana, Prometheus/Mimir, Loki, or Tempo
collectors.

The all-collector prerelease gate must run every supported hosted collector
family that is in scope for the release. A passing collector summary requires
both a rendered Compose service and a positive source or warning fact from that
collector's expected evidence family.

The checked-in base profile does not start those six services by default.
PagerDuty and Jira render from `docker-compose.remote-e2e.yaml`; Grafana,
Prometheus/Mimir, Loki, and Tempo render from
`docker-compose.remote-e2e.observability.yaml`. Operator-local env files
provide private target configuration. Without a rendered hosted service, the
manifest classifies the row as `skipped` with
`collector service disabled in remote Compose profile`.

## Render Profiles

Keep private target values in the operator-local env file, set the matching
enable flag, and render only the intended profiles:

```bash
export ESHU_REMOTE_E2E_ENV_FILE=path/to/private-remote-e2e.env

docker compose --env-file "${ESHU_REMOTE_E2E_ENV_FILE}" \
  -f docker-compose.remote-e2e.yaml \
  -f docker-compose.remote-e2e.observability.yaml \
  --profile jira \
  --profile pagerduty \
  --profile grafana \
  --profile prometheus-mimir \
  --profile loki \
  --profile tempo \
  config --services
```

When running `scripts/verify_remote_e2e_runtime_state.sh` for that stack, keep
the same Compose files and profile set:

```bash
export ESHU_REMOTE_E2E_COMPOSE_FILES="docker-compose.remote-e2e.yaml:docker-compose.remote-e2e.observability.yaml"
export ESHU_REMOTE_E2E_COMPOSE_PROFILES="jira,pagerduty,grafana,prometheus-mimir,loki,tempo"
scripts/verify_remote_e2e_runtime_state.sh
```

The private env file sets only generic runtime knobs in public evidence and
keeps provider URLs, tenant names, service names, account IDs, tokens, label
values, tag values, and raw source details local.

The observability overlay also renders a matching `collector-*-preflight`
service for Grafana, Prometheus/Mimir, Loki, and Tempo. The preflight must
complete before the collector starts. It fails with sanitized output when a
profile is selected but the matching `ESHU_REMOTE_E2E_*_ENABLED` flag is not
`true`, the base URL is blank, or a configured token or tenant env variable is
missing.

No-Regression Evidence: baseline remote observability Compose rendered each
profile-selected collector directly with `restart: on-failure`, so a disabled
or incomplete selected instance could restart-loop before any source work
started. After the preflight change, `scripts/test-remote-e2e-observability-preflight.sh`,
`scripts/test-remote-e2e-hosted-compose-render.sh`, and
`go test ./internal/runtime -run 'TestRemoteE2EObservabilityComposeGatesCollectorsOnPreflight|TestRemoteE2EComposeDefaultsAllowDisabledHostedCoordinatorStartup|TestRemoteE2EComposeJiraUsesJQLEnvReference' -count=1`
prove the four observability profiles render one-shot preflights, gate the
collector dependency with `service_completed_successfully`, and fail disabled
or incomplete target inputs once with sanitized output. Backend/version and
input shape are the NornicDB remote E2E Compose runtime, four hosted
observability profiles, checked-in disabled defaults, and operator-private env
names. Terminal queue and row counts are unchanged because the preflight exits
before collector claims or graph writes. This is safe because collector
binaries remain fail-closed and no worker count, batch size, lease, retry, or
graph-write behavior changes.

No-Observability-Change: existing collector health checks, metrics ports, and
worker logs remain unchanged. The new preflight only emits a sanitized
configuration status or failure before the collector starts; it does not expose
provider URLs, tenants, labels, tokens, queue rows, or graph facts.

## Provider Env

Use `ESHU_REMOTE_E2E_JIRA_ENABLED=true` with `ESHU_JIRA_SITE_ID`,
`ESHU_JIRA_EMAIL`, `ESHU_JIRA_API_TOKEN`, `ESHU_JIRA_JQL`, and optional
bounded Jira limits. The checked-in Compose profile passes the JQL variable
name through `jql_env`, so a private JQL string with spaces or operators stays
in the operator env file instead of being interpolated into
`ESHU_COLLECTOR_INSTANCES_JSON`.

Use `ESHU_REMOTE_E2E_PAGERDUTY_ENABLED=true` with
`ESHU_PAGERDUTY_ACCOUNT_ID`, `ESHU_PAGERDUTY_API_TOKEN`, and optional bounded
PagerDuty limits.

Use `ESHU_REMOTE_E2E_GRAFANA_ENABLED=true` with `ESHU_GRAFANA_BASE_URL`,
`ESHU_GRAFANA_TOKEN`, and bounded Grafana limits.

Use `ESHU_REMOTE_E2E_PROMETHEUS_MIMIR_ENABLED=true` with
`ESHU_PROMETHEUS_MIMIR_BASE_URL`. Set
`ESHU_PROMETHEUS_MIMIR_TOKEN_ENV=PROMETHEUS_MIMIR_TOKEN` and
`ESHU_PROMETHEUS_MIMIR_TENANT_ID_ENV=PROMETHEUS_MIMIR_TENANT_ID` only when the
target requires those values.

Loki and Tempo follow the same optional `*_TOKEN_ENV` and `*_TENANT_ID_ENV`
pattern, with public-safe JSON arrays for allowlisted label or tag names.
Default checked-in values leave all six hosted provider collectors disabled
and blank.

## Manifest Rows

The harness distinguishes the non-passing cases:

- `fail`: the hosted collector service is enabled in the rendered Compose
  profile, but no source facts were observed; or source facts appear even
  though the rendered Compose profile does not include that service.
- `skipped`: the hosted collector service is absent from the rendered Compose
  profile, with public issue reference `#1375`.
- `unsupported`: the operator explicitly listed the row in
  `ESHU_REMOTE_E2E_UNSUPPORTED_HOSTED_COLLECTORS` because that hosted collector
  is not runnable in this proof yet, with public issue reference `#1375`.

Use only public row names in
`ESHU_REMOTE_E2E_UNSUPPORTED_HOSTED_COLLECTORS`, separated by commas:
`pagerduty`, `jira`, `grafana`, `prometheus_mimir`, `loki`, and `tempo`. Do not
put provider URLs, tenant names, instance IDs, tokens, hostnames, or target
labels in this variable or in the generated manifest.

A `skipped` or `unsupported` hosted row makes the generated manifest `partial`,
not `pass`; that is useful evidence for planning, but it is not a clean
all-collector prerelease gate. Do not list an enabled hosted collector as
unsupported to hide missing source facts. Enabled services with zero facts are
failed proof. Do not treat source facts as passing evidence when the rendered
Compose profile lacks the matching service; that is contradictory stale or
out-of-profile evidence and is also a failed row.
