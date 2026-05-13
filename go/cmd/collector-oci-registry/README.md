# collector-oci-registry

## Purpose

`collector-oci-registry` scans OCI Distribution-compatible registry
repositories and emits `oci_registry` facts through the shared collector commit
boundary. It can run the older configured-target polling path or, when
`ESHU_COLLECTOR_INSTANCES_JSON` is present, the claim-aware workflow path. It
covers the JFrog Artifactory Docker/OCI, ECR, Docker Hub, GHCR, Harbor, Google
Artifact Registry, and Azure Container Registry runtime lane.

## Ownership Boundary

This binary owns process wiring only: target JSON parsing, collector-instance
selection, provider client construction, `collector.Service` or
`collector.ClaimedService` setup, Postgres ingestion, telemetry, and the hosted
status surface. Fact identity and envelope construction live in
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

Configured-target mode required values:

- `ESHU_OCI_REGISTRY_COLLECTOR_INSTANCE_ID`
- `ESHU_OCI_REGISTRY_TARGETS_JSON`

Optional values:

- `ESHU_OCI_REGISTRY_POLL_INTERVAL` (Go duration, defaults to `5m`)

Claim-aware mode is selected when `ESHU_COLLECTOR_INSTANCES_JSON` is non-empty.
The runtime selects the `oci_registry` collector instance, requires
`claims_enabled=true`, and uses:

- `ESHU_OCI_REGISTRY_COLLECTOR_INSTANCE_ID` when multiple OCI instances exist
- `ESHU_OCI_REGISTRY_POLL_INTERVAL` for idle claim polling
- `ESHU_OCI_REGISTRY_CLAIM_LEASE_TTL`
- `ESHU_OCI_REGISTRY_HEARTBEAT_INTERVAL`
- `ESHU_OCI_REGISTRY_COLLECTOR_OWNER_ID`

In claim-aware mode the target list comes from the selected collector
instance's `configuration.targets` array. The workflow coordinator plans one
work item per configured registry repository target.

Each target JSON object supports:

- `provider`: `jfrog`, `ecr`, `dockerhub`, `ghcr`, `harbor`,
  `google_artifact_registry`, or `azure_container_registry`
- `repository`
- `references` or `tag_limit`
- provider fields such as `base_url` and `repository_key` for JFrog,
  `base_url` for Harbor, `registry_host` for Google Artifact Registry or Azure
  Container Registry, or `registry_host`, `registry_id`, `region`, and
  `aws_profile` for ECR
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

`/admin/status` also includes OCI rows in `registry_collectors`. Those rows show
configured instance count, active scope count, completed generation count for
the last 24 hours, last completed timestamp,
retryable and terminal failure counts, and bounded failure classes such as
`registry_auth_denied`, `registry_not_found`,
`registry_rate_limited`, `registry_retryable_failure`, `registry_canceled`, and
`registry_terminal_failure`. Status messages and details keep registry hosts,
repository paths, tags, digests, account IDs, credential environment variable
names, and credential values out of the operator payload.

## Invariants

- The collector is read-only. It only calls Distribution and provider auth
  read APIs.
- Facts must flow through `collector.Service`; do not write facts directly from
  this command package. Claim-aware mode flows through
  `collector.ClaimedService` and the same Postgres ingestion store with claim
  fencing.
- Missing Referrers API support is warning evidence, not proof of no SBOMs,
  signatures, or attestations.
- Provider adapters may change auth or endpoint shape, but fact identity stays
  provider-neutral.
- Harbor, Google Artifact Registry, and Azure Container Registry adapters only
  normalize endpoint/auth shape; registry reads still go through the shared
  OCI Distribution client.

## Related Docs

- [OCI registry ADR](../../../docs/docs/adrs/2026-05-10-oci-container-registry-collector.md)
- [Telemetry reference](../../../docs/docs/reference/telemetry/index.md)
- [Service runtimes](../../../docs/docs/deployment/service-runtimes.md)
