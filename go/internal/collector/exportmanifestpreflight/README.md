# Export Manifest Preflight

## Purpose

`collector/exportmanifestpreflight` classifies offline documentation export
manifests before any issue, ticket, chat, or workspace export parser reads
export content. It gives future importers a metadata-only guard for explicit
source scope, ACL posture, bounded file lists, unsafe paths, nested archives,
attachment references, private conversation metadata, and credential-looking
members or token-bearing URLs.

## Ownership boundary

This package owns pure preflight classification for JSON import manifests. It
reads the manifest bytes under a small source-byte limit and returns only
low-cardinality warning classes plus bounded counts.

It does not open listed files, unpack archives, call live providers, discover
repositories, emit documentation facts, persist rows, write graph state, expose
API or MCP routes, add runtime knobs, or enable hosted or repository ingestion.
Offline export parsers, fact emission, ACL enforcement, and live collectors
belong in separate security-reviewed slices.

## Exported surface

See `doc.go` for the godoc-rendered package contract.

- `Options` sets source-byte and file-count budgets.
- `Result` reports safe/unsafe state, supported source system, ACL policy,
  bounded counts, and warning classes.
- `Warning` records a stable low-cardinality class and count.
- `Preflight` classifies one JSON manifest from an `io.Reader`.
- Source-system constants cover GitHub, Jira, Slack, Teams, Google Workspace
  export, and generic documentation export manifests.
- Warning constants mirror the #1741/#1748 design classes for invalid manifests,
  unsupported formats, explicit allowlists, ACL state, unsafe paths, nested
  archives, attachment metadata, private channel metadata, duplicate source
  items, redaction, resource limits, and timeout.

## Dependencies

The package uses only the Go standard library. `encoding/json` decodes the
bounded manifest, while path checks stay local and do not touch the filesystem.

## Telemetry

None directly. This package has no runtime side effects and no metric, log, or
trace emission. Future importer wiring must record attempted manifest
preflights, warning classes, file counts, elapsed time, source bytes, duplicate
item counts, redaction counts, ACL partial/unavailable counts, and
resource-limit outcomes through
collector telemetry before enabling offline export ingestion.

Collector Performance Evidence: `go test ./internal/collector/exportmanifestpreflight
-count=1` proves manifest preflight is bounded by source bytes and file count.

Collector Observability Evidence: this package emits no facts, metrics, spans,
logs, status rows, or graph writes. Future importer wiring must add runtime
collector signals before any long-running or hosted ingestion path is enabled.

No-Observability-Change: this package is a pure metadata preflight helper. It
adds no worker, queue consumer, graph write, database query, HTTP handler, MCP
tool, runtime stage, provider client, or hosted collector path.

## Gotchas / invariants

- Preflight never stores raw `source_scope_id`, source cursor, issue keys,
  channel names, message IDs, file paths, private URLs, user names, tenant
  names, source item IDs, tokens, credentials, or export content in `Result`.
- A non-empty `files` list is an allowlist. Empty lists and broad scopes fail
  closed; blank configuration never means all provider content.
- Attachment references are metadata-only until a later extractor and security
  review explicitly approve binary attachment ingestion.
- Partial or unavailable ACL state makes the manifest unsafe for content
  extraction; later evidence reads must deny content by default.
- `export_archive_malformed` intentionally covers nested archive references in
  this package because nested export packages are rejected before any archive
  preflight or unpacking step.
- Duplicate source item IDs and token-bearing URLs are counted from manifest
  metadata only. The raw IDs and URLs are not returned.
- A safe preflight result is not approval to ingest. It only means this guard
  did not find a disqualifying manifest-level issue.

## Related docs

- `docs/internal/design/1741-1748-google-workspace-and-external-export-ingestion.md`
- `docs/public/reference/local-testing.md`
