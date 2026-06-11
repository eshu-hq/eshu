# Google Workspace Collector Boundary

## Purpose

`internal/collector/googleworkspace` proves the default-off Google Workspace
documentation boundary with mocked clients and synthetic source identities. It
turns explicitly allowlisted Docs, Sheets, and Slides exports into
source-neutral documentation facts without adding live provider access.

## Ownership boundary

This package owns allowlist validation, export MIME mapping, ACL summarization,
failure-class normalization, source identifier redaction, and documentation fact
construction for mocked Google Workspace inputs. It does not own credentials,
HTTP clients, Drive pagination, runtime flags, Helm or Compose settings,
repository discovery, graph writes, reducer materialization, API routes, or MCP
tools.

## Exported surface

See `doc.go` for the godoc-rendered package contract.

- `Client` is the mocked Drive evidence boundary.
- `Allowlist` describes explicit file, folder, or shared-drive scopes.
- `Request` and `Result` describe one collection attempt.
- `File`, `PermissionSummary`, `Export`, `Section`, and `Link` are normalized
  source inputs.
- `Collect` emits documentation envelopes only.

## Dependencies

The package depends on `internal/facts` for documentation fact payloads and
`internal/scope` for the collector kind. It uses only the Go standard library
otherwise.

## Telemetry

None directly. This package has no runtime loop and emits no metrics, spans,
logs, status rows, queues, HTTP requests, or provider calls.

Collector Performance Evidence: `go test ./internal/collector/googleworkspace
-count=1` covers mocked allowlist validation, per-file export processing,
failure classification, redaction, and fact construction with bounded in-memory
fixtures only.

Collector Observability Evidence: runtime telemetry is intentionally absent
because the boundary is not wired to a hosted collector. Any later live collector
must add bounded attempt counters, failure-class counters, ACL partial/denied
counters, export bytes, elapsed time, status rows, logs, spans, and existing
fact emission counters before enablement.

Collector Deployment Evidence: no deployment artifact changes in this package.
It adds no Compose profile, Helm values, container image, scheduled worker,
credential mount, ServiceMonitor, or public operator enablement path.

No-Observability-Change: this package adds no worker, queue consumer, database
query, graph write, HTTP handler, MCP tool, runtime flag, hosted collector path,
metric, span, log, or status row.

## Gotchas / invariants

- Blank allowlists stay disabled and never mean all Drive.
- Shared-drive collection requires an explicit bounded query string in addition
  to drive IDs.
- Raw file IDs, principals, tenant URLs, token-bearing links, and private URLs
  are fingerprinted or redacted before facts are emitted.
- Docs export to DOCX, Sheets export to XLSX, and Slides export to PPTX.
- Per-file failures produce metadata-only document facts and do not emit
  sections.
- Document ACL summaries carry the bounded `source_acl_state`
  (`allowed|denied|partial|missing|stale`) derived from the observed posture:
  permission-denied or download-denied maps to `denied`, deleted or trashed
  maps to `missing`, a stale revision maps to `stale`, and a partial ACL read
  maps to `partial`. A successful read does not collect the full restriction
  set, so the field is omitted rather than upgraded to `allowed`.
- The emitted facts remain documentation evidence only and must not become
  operational graph truth inside this package.

## Related docs

- `docs/internal/design/1741-1748-google-workspace-and-external-export-ingestion.md`
- `docs/internal/design/1737-visual-media-documentation-ingestion.md`
- `go/internal/collector/README.md`
