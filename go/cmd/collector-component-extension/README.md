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

For the `vulnerability_intelligence` kind only, the worker also sets a
`extensionhost/vulnerability.Recorder` as the host's `ClaimOutcomeRecorder` (#9420),
built on `vulnerabilitystore.NewVulnerabilitySourceStateStore(database)` like the
retired in-tree command. It writes `vulnerability_source_states` under this
worker's instance id: the attempt or failure row from each claim outcome and
the success row after the durable commit, at most two single-row statements per
claim. `next_retry_at` and terminality come from `collector.ClaimRetryDelay` and
`collector.ClaimAttemptBudgetExhausted` with this worker's poll interval and the
default attempt budget, the same helpers `ClaimedService` applies to the claim. Writes record the
`vulnerability_intelligence.source_state_checkpoint` span event; skipped stale
writes and store failures count on
`eshu_dp_vulnerability_source_state_stale_writes_skipped_total` and
`eshu_dp_vulnerability_source_state_write_failures_total` (labels `status`,
`result`). Every other kind gets no recorder.

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

No-Regression Evidence (#9185 follow-up): moving the three entries between
registry blocks changes only documentation grouping (the coverage gate keys
on variable names, not subsystems) and the static-key conflict pin adds one
startup-rejection unit test; no runtime path changes. The regenerated
reference docs move the same three rows between tables and are otherwise
byte-identical.

No-Observability-Change (#9185 follow-up): the follow-up adds no metric,
span, log line, or status field; the new test asserts the existing
fail-closed startup error, so operator signals are unchanged.

No-Regression Evidence (#9413): making `scrubHostCredentialEnv` return
`(Runner, error)` adds one startup type switch before `NewSource`; the
per-claim path is unchanged (same scrubbed runner reaches the same child-env
filter, same explicit-Env append). Baseline and after both run `go test
-count=1 ./cmd/collector-component-extension
./internal/collector/extensionhost ./internal/component` green with `go vet`
and `gofmt` clean, including the stacked #9258 wired-path proof that execs a
real satellite child. The input shape is the closed set of in-tree runner
types (process value/pointer, OCI value/pointer) plus a fake inheriting
runner; no queue, lease, batching, Cypher, graph-write, or network surface is
touched, so claim throughput and terminal counts are unaffected.

No-Observability-Change (#9413): the guard adds no metric, span, log line,
or status field; an unknown runner fails startup with an error naming only
the Go type, reusing the existing `buildClaimedService` startup-error path,
so operators keep diagnosing through the existing claim rows, failure
classes, and service metrics endpoint.

## Related docs

- `docs/public/extend/community-extension-authoring.md`
- `docs/public/reference/component-package-manager.md`
- `docs/public/deployment/service-runtimes-collectors.md`
- `docs/public/run-locally/docker-compose.md`

## Workflow Claim Store

By default this collector stays on the core Postgres workflow claim store.
It passes a `CoreOnlyReason` to `store.New` because its slot-scope resolver
reads `workflow_runs.requested_scope_set`, which only core Postgres holds.
Setting `ESHU_WORKFLOW_CLAIM_PLANE_URL` without the enabled flow below stops
startup with an error that names the variable and that reason (#8875).

### Enabled flow (eshu-hq/eshu#8877 leaf 1, default off)

Setting `ESHU_EXTENSIONHOST_CUTOVER_ENABLED` to a true Go boolean spelling
lifts the refusal for one designed path only, and only when every route is
bound:

- `ESHU_WORKFLOW_CLAIM_PLANE_URL` set: claims flow through the claim-plane
  client over `workflow.v1`, and commits take the plane commit fence pinned
  on that client.
- `ESHU_EXTENSIONHOST_CUTOVER_PRODUCER_ID` and
  `ESHU_EXTENSIONHOST_CUTOVER_PRODUCER_VERSION` set, both equal to the
  verified component manifest: the remote client proves under exactly that
  holder identity. A mismatch fails startup naming only the variable.
- Slot-scope run reads stay on core Postgres by explicit carve-out: the
  resolver binds the core control store in every mode because the plane has
  no run-row route.
- Live-grant re-reads route through the cutover gate; the backing authority
  (the on-disk registry) is unchanged.

Any other URL-set state keeps the refusal, and any unrecognized flag
spelling fails startup. The grant-observer backing that makes seam grant
decisions visible to operators lands with the telemetry leaf (leaf 5):
the enabled client reports wiring (as activation) and emission decisions
to the worker's grant telemetry recorder, reusing
`eshu_dp_component_producer_grant_decisions_total` with no new metric or
label value, and a nil observer disables observation without changing any
outcome. Claim/commit lifecycle operations through
the cutover gate are exercised by the parity and concurrency leaves.

No-Regression Evidence (leaf 1): the wiring resolves once at startup
(flag parse, manifest comparison, one client build when enabled) and adds
no per-claim work beyond one closure indirection to the identical
registry read. Baseline and after both run `go test -count=1
./cmd/collector-component-extension` and `go test -race -count=1` on the
touched packages green. Input shape is the fixture registry with a dummy
claim-plane URL; construction makes no network calls, and no queue, lease,
batching, Cypher, graph-write, or reducer surface is touched, so claim
throughput and terminal counts are unaffected.

No-Observability-Change (leaf 1): the wiring registers no metric, span,
log line, or status field. Selection stays on the store-selection counter
and log, grant and claim outcomes stay on the existing grant and run
signals, and the stage is named in the telemetry-coverage row with the
same marker.
