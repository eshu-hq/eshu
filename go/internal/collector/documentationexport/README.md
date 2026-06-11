# Documentation Export Collector Helpers

## Purpose
This package parses explicit offline issue, ticket, and chat export records into
source-neutral documentation facts. It exists for the default-off external
export lane tracked in the Google Workspace and external export design note.

## Ownership boundary
The package owns manifest-gated parsing of caller-supplied JSON export records
for GitHub, Jira, Slack, Teams, Google Workspace export, and generic
documentation export sources. It does not discover files, unpack archives, call
live provider APIs, read credentials, emit graph truth, or classify prose as
canonical incident, deployment, ownership, or work-item state.

## Exported surface
`doc.go` describes the godoc contract. The exported surface is `Request`,
`Result`, and `Collect`; callers provide manifest bytes and an exact file byte
map, then receive preflight evidence plus documentation fact envelopes.

## Dependencies
The package depends on `go/internal/collector/exportmanifestpreflight` for
allowlist, ACL, path, attachment, private-channel, and sensitive-value guards.
It emits payloads from `go/internal/facts` and marks envelopes with
`scope.CollectorDocumentation`.

## Telemetry
No-Observability-Change: this package has no runtime loop, queue, worker,
metrics, spans, logs, status rows, provider calls, or graph writes. Existing
fact emission counters cover any future caller that commits returned envelopes.

Collector Performance Evidence: parser work is bounded by manifest preflight
limits, explicit file allowlists, and per-section truncation. Local proof uses
synthetic one-file and two-file manifests through
`go test ./internal/collector/documentationexport -count=1` plus the combined
collector/query/MCP package test.

Collector Observability Evidence: no new metrics, spans, logs, or status rows
are introduced because this is a parser-only helper. Future runtime callers that
commit returned envelopes use the existing fact emission counters and collector
commit signals.

Collector Deployment Evidence: no Compose, Helm, ServiceMonitor, CLI flag, or
runtime profile changes are introduced. The package remains default-off until a
separate security-reviewed runtime integration adds deployment configuration.

## Gotchas / invariants
Manifest preflight is fail-closed; any warning returns no facts. Record parsing
is metadata-only for malformed or unsupported record shapes, and those records
never emit sections. Raw provider scope IDs, item IDs, and export paths are
represented with stable fingerprints in metadata. Document `content_hash`
values include the raw source record bytes so nested comments, changelog
entries, and chat messages affect revision identity. Unknown source scope kinds
are stored as fingerprints instead of raw values. Section truncation preserves
valid UTF-8 before hashes are computed. Link facts keep only `export:` section
references, do not retain source-native message or comment IDs as section IDs,
and redact token-bearing URLs, credential-looking local paths, and local
absolute paths. Attachment binaries and private-channel content are not parsed
here because preflight rejects those manifests before file bytes are inspected.
Source and document ACL summaries carry the bounded `source_acl_state`
(`allowed|denied|partial|missing|stale`) derived from the manifest ACL policy:
an evaluated policy (`source_acl_evaluated`) is the only documentation producer
that asserts `allowed`, a partial policy maps to `partial`, and an unavailable
policy observes no posture so the field is omitted (no default-when-unknown
guess; that disclosure decision is reserved for security review).

## Related docs
- `docs/internal/design/1741-1748-google-workspace-and-external-export-ingestion.md`
- `go/internal/collector/exportmanifestpreflight`
