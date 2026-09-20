# Collector preflight contracts

## Purpose

`collector/preflight` groups the collector-side safety classifiers used to
inspect bundled documentation payloads before extraction. It is a
documentation-only namespace; implementation belongs in its leaf packages and
collection behavior stays in the owning collector packages.

## Ownership boundary

The namespace owns no runtime declarations, extraction, provider access,
fact emission, storage calls, graph writes, or telemetry. Its `archive`
child owns only metadata-only classification of `.zip`, `.tar`, `.tar.gz`,
and `.tgz` packages. Its `pdf` child owns only metadata-only classification
of `.pdf` sources. Its `diagram` child owns only metadata-only
classification of `.svg`, `.drawio`, `.excalidraw`, `.mmd`, `.mermaid`,
`.puml`, `.plantuml`, and `.d2` sources. Its `picture` child owns only
metadata-only classification of `.png`, `.jpeg`, `.jpg`, and `.gif` sources.
Its `media` child owns only metadata-only classification of audio and
video sources. Its `ooxml` child owns only metadata-only classification
of `.docx`, `.xlsx`, and `.pptx` sources.

## Exported surface

None. This parent is documentation-only. See the `archive`, `pdf`,
`diagram`, `picture`, `media`, and `ooxml`
children for `Options`, `Result`, `Warning`, `Preflight`, and the format
and warning constants.

## Dependencies

None. The parent `doc.go` contains only package documentation and its package
clause. Dependencies belong to leaf packages.

## Telemetry

None. This namespace executes no runtime code and changes no observability
contract.

## Gotchas / invariants

- Keep runtime declarations in leaf packages or the owning collector.
- Keep extraction, fact emission, ACL handling, and telemetry behavior in the
  owning collector slice.
- The `archive`, `pdf`, `diagram`, `picture`, `media`, and `ooxml` leaves are preflight boundaries, not
  independently deployable services. They must not import the collector root
  or a sibling collector.
- A safe preflight result is necessary but not sufficient for ingestion.

## Related docs

- `go/internal/collector/preflight/archive/README.md`
- `go/internal/collector/preflight/pdf/README.md`
- `go/internal/collector/preflight/diagram/README.md`
- `go/internal/collector/preflight/picture/README.md`
- `go/internal/collector/preflight/media/README.md`
- `go/internal/collector/preflight/ooxml/README.md`
- `go/internal/collector/README.md`
- `docs/internal/design/1738-office-spreadsheet-deck-archive-ingestion.md`
