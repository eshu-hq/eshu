# PagerDuty Reference Component

## Purpose

This fixture-only package shows how an out-of-tree PagerDuty collector can emit
`collector-sdk/v1alpha1` source evidence without live credentials. It uses
redacted synthetic observations and namespaced fact kinds, then the in-tree
PagerDuty parity test compares the emitted payloads with the core PagerDuty
fact contract.

## Ownership boundary

The package owns fixture parsing, deterministic SDK result construction, and
local component manifest proof. It does not call PagerDuty, read credentials,
schedule hosted work, claim workflow rows, write graph truth, or expose API/MCP
readback.

## Exported surface

- `ComponentID`, `CollectorKind`, `SourceSystem`, `MetricsPrefix` - manifest
  and claim identity constants.
- `Contract` - SDK fact families declared by the component manifest.
- `LoadObservation` - decode one redacted synthetic observation fixture.
- `Collect` - convert a fixture observation into a collector SDK result.

See `doc.go` for the godoc contract.

## Dependencies

- `github.com/eshu-hq/eshu/sdk/go/collector` - public SDK wire records and
  validator.

The package imports no Eshu `go/internal` packages.

## Telemetry

None. No-Observability-Change: this reference package has no runtime service,
provider client, queue consumer, graph writer, metric registration, span, or
log path. The manifest reserves `eshu_dp_example_pagerduty_` for future
component-owned metrics only.

## Gotchas / invariants

- Component manifests cannot claim core-owned fact kinds, so the emitted kinds
  are namespaced under `dev.eshu.examples.pagerduty.*`.
- The manifest artifact image is a digest-pinned placeholder for local manifest
  validation only. This package does not publish an OCI image or prove hosted
  execution.
- Stable keys, source references, schema versions, source confidence, and
  payload fields mirror the in-tree PagerDuty envelopes where the public SDK
  allows the same shape.
- The SDK rejects sensitive-looking payload keys. Routing-key redaction is
  represented with a `Redaction` entry instead of an in-payload
  `routing_key_redacted` field.
- Fixtures must remain synthetic and free of real PagerDuty IDs, incident
  titles, responder identities, service names, tokens, routing keys, provider
  payloads, private endpoints, host paths, and IP addresses.
- The reducer contract is `source_evidence_only:no_graph_truth`; these facts
  are provenance only.

## Related docs

- `docs/public/extend/community-extension-authoring.md`
- `docs/public/reference/component-package-manager.md`
- `docs/public/reference/pagerduty-evidence.md`
