# collector-package-registry

## Purpose

`collector-package-registry` runs the claim-aware `package_registry` collector.
It fetches explicit package metadata documents from configured registry targets
and emits package, version, dependency, artifact, source-hint,
vulnerability-hint, registry-event, hosting, and warning facts through the
shared collector commit boundary.

## Ownership Boundary

This binary owns process wiring only: collector-instance selection, target JSON
parsing, credential environment resolution, `collector.ClaimedService` setup,
Postgres ingestion, telemetry, and the hosted status surface. Package identity,
parser registration, and envelope construction live in
`internal/collector/packageregistry`; runtime fetch and claim handling live in
`internal/collector/packageregistry/packageruntime`.

## Entry Points

- `main` and `run` in `go/cmd/collector-package-registry/main.go`
- `loadClaimedRuntimeConfig` in `go/cmd/collector-package-registry/config.go`
- `buildClaimedService` in `go/cmd/collector-package-registry/service.go`
- `go run ./cmd/collector-package-registry` for local verification
- `eshu-collector-package-registry --version` and
  `eshu-collector-package-registry -v` print the build-time version

## Configuration

The collector requires standard Postgres env values plus
`ESHU_COLLECTOR_INSTANCES_JSON`. It selects the `package_registry` instance,
requires `enabled=true` and `claims_enabled=true`, and uses:

- `ESHU_PACKAGE_REGISTRY_COLLECTOR_INSTANCE_ID` when multiple package-registry
  instances exist
- `ESHU_PACKAGE_REGISTRY_POLL_INTERVAL` for idle claim polling
- `ESHU_PACKAGE_REGISTRY_CLAIM_LEASE_TTL`
- `ESHU_PACKAGE_REGISTRY_HEARTBEAT_INTERVAL`
- `ESHU_PACKAGE_REGISTRY_COLLECTOR_OWNER_ID`

Each selected instance's `configuration.targets` array supports:

- `provider`
- `ecosystem`: `npm`, `pypi`, `gomod`, `maven`, `nuget`, or `generic`
- `registry`
- `scope_id`
- `packages`, `package_limit`, and `version_limit`
- `metadata_url`
- credential indirection fields: `username_env`, `password_env`,
  `bearer_token_env`

Credentials are resolved from the named environment variables at runtime and
are not copied into facts, logs, metrics, or docs.

## Telemetry

The binary exposes the shared hosted runtime with `/healthz`, `/readyz`,
`/metrics`, and `/admin/status`. Package-registry collection records:

| Metric | Type | Labels | Purpose |
| --- | --- | --- | --- |
| `eshu_dp_package_registry_observe_duration_seconds` | Float64 histogram | `provider`, `ecosystem`, `result` | Measures one claimed target from fetch through fact envelope construction. |
| `eshu_dp_package_registry_requests_total` | Counter | `ecosystem`, `status_class` | Counts metadata fetch attempts and separates success, error, and rate-limited outcomes. |
| `eshu_dp_package_registry_facts_emitted_total` | Counter | `ecosystem`, `fact_kind` | Counts emitted package-registry fact envelopes by kind. |
| `eshu_dp_package_registry_rate_limited_total` | Counter | `ecosystem` | Counts HTTP 429 metadata responses. |
| `eshu_dp_package_registry_generation_lag_seconds` | Float64 histogram | `ecosystem` | Measures source document observation lag when providers report it. |
| `eshu_dp_package_registry_parse_failures_total` | Counter | `ecosystem`, `document_type` | Counts parser failures by package-native document family. |

Package names, feed URLs, versions, and artifact paths stay out of metric
labels.

`/admin/status` also includes package-registry rows in `registry_collectors`.
Those rows show configured instance count, active scope count, completed
generation count for the last 24 hours, last completed timestamp, retryable and
terminal failure counts, and bounded failure classes such as `registry_auth_denied`,
`registry_not_found`, `registry_rate_limited`, `registry_retryable_failure`,
`registry_canceled`, and `registry_terminal_failure`. Status messages and
details keep package names, private feed URLs, metadata paths, credential
environment variable names, and credential values out of the operator payload.

## Related Docs

- [Package registry ADR](../../../docs/docs/adrs/2026-05-12-package-registry-collector.md)
- [Telemetry reference](../../../docs/docs/reference/telemetry/index.md)
- [Service runtimes](../../../docs/docs/deployment/service-runtimes.md)
