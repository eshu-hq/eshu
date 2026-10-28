# Scanner Component (OS Runtime)

## Purpose

`scanner-component-osruntime` executes the in-tree OS package analyzer
(`os_package_extraction`) behind the `collector-sdk/v1alpha3` stdio contract. The
core extension host launches one process per call with a JSON request on
stdin; the binary writes exactly one result page document to stdout and
repeats the analysis deterministically when the host resumes with a page
cursor. Refs #8878, #9385.

## Ownership boundary

This binary owns stdio transport only. Analyzer truth lives in
`internal/collector/ospackagevulnerability/osruntime`; the request/page contract
lives in `internal/collector/scannerworker/component`. The binary holds no
claim, no store handle, and no network client: the host validated the claim,
the activation, and the producer grants before launch. It emits source facts
only and never reducer findings or graph truth.

## Exported surface

The command package exposes no library API. `main` parses the `--version`
flag, then delegates to `component.Runner` pinned to the image component
identity.

## Dependencies

- `internal/collector/scannerworker/component` for the runner, the
  per-analyzer manifest, and the required grants.
- `internal/collector/ospackagevulnerability/osruntime` (via the runner)
  for installed-package evidence over already-extracted rootfs metadata
  (no image pulling, no registry credentials).
- `internal/buildinfo` for the `--version` flag.

## Telemetry

Observability travels in-band: every call emits one SDK status record
(complete with fact count and source latency, or failure with a bounded
class) plus one structured stderr log line carrying the component identity,
outcome, and counts only. Claim values (scope IDs, image references,
paths, digests) never appear in logs. The stderr line is a debugging aid,
not an operator signal: the host captures it in a bounded 64 KiB per-call
buffer that only annotates the failure message and is never read on
success. Host-side spans, metrics, and grant telemetry remain the operator
surfaces.
