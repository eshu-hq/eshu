# Component Extension Collector

## Purpose

`collector-component-extension` runs trusted, claim-capable component
activations through the collector SDK host. It consumes workflow
claims planned by the workflow coordinator, launches the manifest-declared
adapter (process or OCI) with a bounded JSON request, validates the SDK result,
and commits accepted facts through the normal claimed collector boundary.

## Ownership boundary

This binary owns component activation readback, process-runner configuration,
and claim-aware host wiring for public collector SDK extensions. It does not
install components, publish packages, embed an OCI registry client, verify
Sigstore provenance, write graph truth, or expose API/MCP inventory. The OCI
adapter delegates the digest-pinned image pull and run to the host container
runtime (`docker`/`podman`). Reducers remain the only owner of graph nodes and
relationships derived from extension facts.

## Exported surface

The command package exposes no library API. Its internal wiring centers on
`loadRuntimeConfig`, `buildClaimedService`, and `newClaimID`; godoc in
`doc.go` describes the binary contract.

## Dependencies

- `internal/component` for registry readback, trust policy, manifests, and the
  shared activation config handle.
- `internal/collector/extensionhost` for SDK request/result validation and
  process execution.
- `internal/collector` for `ClaimedService` claim, retry, heartbeat, and commit
  behavior.
- `internal/storage/postgres` for workflow claim mutation and durable fact
  commits.
- `internal/runtime`, `internal/app`, and `internal/telemetry` for hosted
  process wiring.

## Telemetry

The binary uses service name `collector-component-extension`, status server
name `collector-component-extension`, and Postgres store name
`collector_component_extension`. It inherits `/healthz`, `/readyz`, `/metrics`,
`/admin/status`, OTEL spans, and collector commit metrics from shared runtime,
collector, and storage packages. The worker also attaches one
`extensionhost.GrantTelemetry` observer, so registry readback, extension-host
activation, and every per-emission recheck record
`eshu_dp_component_producer_grant_decisions_total` (labels `decision`, `stage`,
`reason`, `fact_kind`), a `component.producer_grant.decision` span event, and
`producer_grant.*` log keys. Producer id stays off metric labels. See
[Component Producer Grants](../../../docs/public/reference/component-producer-grants.md#grant-decision-telemetry).

The worker sets `extensionhost.StatusTelemetry` as the status recorder (#8908),
so each SDK status row and claim result records the
`eshu_dp_component_extension_*` instruments (labels `collector_kind`, `state`,
`status_class`, `failure_class`) and one `component extension result` log line
with `extension_status.*` keys. A retryable result's largest
`retry_after_seconds` (capped at one hour) paces the claim retry, as the larger
of the poll interval and that request, until the attempt budget is spent; the
counter `eshu_dp_workflow_claim_provider_throttle_total{outcome="retry_after_honored"}`
fires when the applied delay exceeds the poll interval. The status gauge and
log line record the request, not the applied delay.
`TestBuildClaimedServiceRecordsExtensionStatusAndHonorsRetryAfter` proves the
wiring end to end through the real process runner.

## Gotchas / invariants

- `ESHU_COMPONENT_HOME` is required and must point at the same registry the
  workflow coordinator used while planning work.
- Trust policy fails closed. The worker skips untrusted, revoked, incompatible,
  failed, disabled, or non-claim-capable activations.
- The process adapter reads a local activation config file. The raw path stays
  in registry state and is converted to a stable config handle before workflow
  rows or SDK claims see it.
- The optional activation `host` block is the only config content promoted into
  workflow planning. It may name `sourceSystem`, `scope.id`, and `scope.kind`
  so the SDK claim matches the external source instead of a synthetic component
  scope.
- The worker mints one host-owned GitHub App installation token per claim only
  when `ESHU_GITHUB_APP_ID`, `ESHU_GITHUB_APP_PRIVATE_KEY`, and
  `ESHU_GITHUB_APP_INSTALLATIONS` (a JSON object mapping scope IDs to
  installation IDs) are all set; otherwise the static config path applies.
  Malformed installations JSON fails startup closed. The App private key never
  crosses the extension stdio boundary. An activation must not define the
  `host_github_app_token` key itself while the provider is set: the host
  rejects that combination at startup, so the minted per-claim token is the
  only credential a host_minted target can spend.
- The worker supports `spec.runtime.adapter: process` and
  `spec.runtime.adapter: oci`. The OCI image is read only from the component's
  verified manifest artifact (digest-pinned and platform-matched); the
  activation config file supplies isolation knobs (`oci.network`, `oci.user`,
  `oci.env`, `oci.extra_args`, stdout/stderr limits) but never the image, so a
  config edit cannot repoint the worker at an unverified artifact. Publishing
  the reference image and remote-Compose execution proof are tracked separately
  (#1923/#2126).
- Claim retries, terminal failure, stale fencing, heartbeats, and completion
  are owned by `collector.ClaimedService`; do not bypass that boundary.

No-Regression Evidence: `go test ./cmd/collector-component-extension -run 'TestLoadRuntimeConfig|TestRunnerForAdapter|TestOCIArtifactImage|TestBuildClaimedService' -count=1` proves the worker selects one trusted claim-capable activation, builds a `ProcessRunner` for the process adapter and a digest-pinned `OCIRunner` (image from the verified manifest artifact) for the oci adapter, rejects unsupported adapters, applies activation host scope metadata, wires `extensionhost.Source`, and preserves `collector.ClaimedService` max-attempt behavior for the component claim conflict domain.

No-Observability-Change: beyond the extension status signals above, the worker
adds no new metric labels, queue domains, graph writes, or API/MCP routes. Operators diagnose progress and failure
through existing collector `/admin/status`, workflow claim rows, failure
classes, commit counters, and the service metrics endpoint.

No-Regression Evidence (#9185): wiring `tokenProviderFromEnv` into
`buildClaimedService` adds one startup env read plus one nil check per claim;
the mint/inject path it enables is covered by the owning `extensionhost`
suite. Baseline and after both run `go test -count=1
./cmd/collector-component-extension/... ./internal/collector/extensionhost/...`
green with `go vet` and `gofmt` clean; the input shape is three operator env
vars with no queue, lease, batching, Cypher, or graph-write surface, so the
claim hot path is unchanged when the vars are absent (nil-provider early
return) and adds one bounded mint call per claim when set.

No-Observability-Change (#9185): the wiring adds no new metric, span, log
line, or status field; startup misconfiguration fails with an error naming
only the variable, and per-claim mint faults reuse the existing bounded
terminal/retryable failure classes, so operators keep diagnosing through the
existing claim rows, failure classes, and service metrics endpoint.

## Related docs

- `docs/public/extend/community-extension-authoring.md`
- `docs/public/reference/component-package-manager.md`
- `docs/public/deployment/service-runtimes-collectors.md`
- `docs/public/run-locally/docker-compose.md`
