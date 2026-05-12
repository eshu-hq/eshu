# collector-oci-registry

## Purpose

`collector-oci-registry` scans configured OCI Distribution-compatible registry
repositories and emits `oci_registry` facts through the shared collector commit
boundary. It covers the initial JFrog Artifactory Docker/OCI, ECR, Docker Hub,
and GHCR runtime lane.

## Ownership Boundary

This binary owns process wiring only: target JSON parsing, provider client
construction, `collector.Service` setup, Postgres ingestion, telemetry, and the
hosted status surface. Fact identity and envelope construction live in
`internal/collector/ociregistry`; registry scan orchestration lives in
`internal/collector/ociregistry/ociruntime`.

## Entry Points

- `main` and `run` in `go/cmd/collector-oci-registry/main.go`
- `loadRuntimeConfig` in `go/cmd/collector-oci-registry/config.go`
- `buildCollectorService` in `go/cmd/collector-oci-registry/service.go`
- `go run ./cmd/collector-oci-registry` for local verification
- `eshu-collector-oci-registry --version` and
  `eshu-collector-oci-registry -v` print the build-time version before runtime
  setup begins

## Configuration

The collector requires standard Postgres env values plus one collector instance
ID and a JSON target list.

Required values:

- `ESHU_OCI_REGISTRY_COLLECTOR_INSTANCE_ID`
- `ESHU_OCI_REGISTRY_TARGETS_JSON`

Optional values:

- `ESHU_OCI_REGISTRY_POLL_INTERVAL` (Go duration, defaults to `5m`)

Each target JSON object supports:

- `provider`: `jfrog`, `ecr`, `dockerhub`, or `ghcr`
- `repository`
- `references` or `tag_limit`
- provider fields such as `base_url` and `repository_key` for JFrog, or
  `registry_host`, `registry_id`, `region`, and `aws_profile` for ECR
- credential indirection fields: `username_env`, `password_env`,
  `bearer_token_env`

Credentials are resolved from the named environment variables at runtime and
are not copied into docs, logs, metrics, or facts.

## Telemetry

The binary exposes the shared hosted runtime with `/healthz`, `/readyz`,
`/metrics`, and `/admin/status`. OCI registry scans record these metrics:

| Metric | Type | Labels | Purpose |
| --- | --- | --- | --- |
| `eshu_dp_oci_registry_api_calls_total` | Counter | `provider`, `operation`, `result` | Counts registry and provider API calls so operators can separate auth, tag-list, manifest, and referrer failures. |
| `eshu_dp_oci_registry_tags_observed_total` | Counter | `provider`, `result` | Counts tags accepted into a bounded scan after tag-list filtering. |
| `eshu_dp_oci_registry_manifests_observed_total` | Counter | `provider`, `media_family` | Counts manifest and index objects observed by broad media family. |
| `eshu_dp_oci_registry_referrers_observed_total` | Counter | `provider`, `artifact_family` | Counts SBOM, signature, attestation, vulnerability, unknown, and other referrer artifacts. |
| `eshu_dp_oci_registry_scan_duration_seconds` | Float64 histogram | `provider`, `result` | Measures end-to-end repository scan latency, excluding the Postgres commit span recorded by `collector.observe`. |

OCI registry scans also emit `oci_registry.scan` and `oci_registry.api_call`
trace spans. Use `oci_registry.scan` to find the slow target and
`oci_registry.api_call` to separate ping, tag-list, manifest, and referrer API
latency.

Metric labels stay bounded to provider, operation, result, media family, and
artifact family. Registry hosts, repositories, tags, and digests stay out of
metric labels.

## Invariants

- The collector is read-only. It only calls Distribution and provider auth
  read APIs.
- Facts must flow through `collector.Service`; do not write facts directly from
  this command package.
- Missing Referrers API support is warning evidence, not proof of no SBOMs,
  signatures, or attestations.
- Provider adapters may change auth or endpoint shape, but fact identity stays
  provider-neutral.

## Related Docs

- [OCI registry ADR](../../../docs/docs/adrs/2026-05-10-oci-container-registry-collector.md)
- [Telemetry reference](../../../docs/docs/reference/telemetry/index.md)
- [Service runtimes](../../../docs/docs/deployment/service-runtimes.md)
