# Archive Preflight

## Purpose

`collector/archivepreflight` classifies bundled documentation archives before
any archive extractor reads member content. It gives the Git documentation
packet path and future archive collectors a metadata-only guard for resource
limits, unsafe paths, symlinks, special files, nested archives,
credential-like members, malformed containers, and cancellation.

## Ownership boundary

This package owns pure preflight classification for `.zip`, `.tar`, `.tar.gz`,
and `.tgz` archives. It reads archive directory/header metadata only. It does
not unpack members to disk, parse contained document bodies, store member
names, emit documentation facts, persist rows, write graph state, expose API or
MCP routes, add runtime knobs, or enable hosted/repository ingestion.

Contained-document routing, extraction, fact emission, ACL behavior, and
security-review enablement belong in the owning collector slice that calls this
preflight helper.

## Exported surface

See `doc.go` for the godoc-rendered package contract.

- `Options` sets source-byte, expanded-byte, entry-count, and
  compression-ratio budgets.
- `Result` reports format, safe/unsafe state, bounded counts, and warning
  classes.
- `Warning` records a stable low-cardinality class and count.
- `Preflight` classifies one archive using an `io.ReaderAt`, source name, byte
  size, context, and options.
- Format constants: `FormatZIP`, `FormatTAR`, and `FormatTARGZ`.
- Warning constants cover unsupported formats, malformed containers, resource
  limits, compression-ratio limits, unsafe paths, symlinks, special files,
  nested archives, credential-like members, and cancellation/deadline timeout.

## Dependencies

The package uses only the Go standard library. `archive/zip` opens ZIP central
directory metadata, `archive/tar` streams tar headers from an `io.SectionReader`
or gzip stream, `compress/gzip` unwraps `.tar.gz` and `.tgz` sources, and
`context` lets callers stop preflight before extraction proceeds.

## Telemetry

None directly. This package has no runtime side effects and no metric, log, or
trace emission. The Git documentation packet path records preflight counts and
warning classes in documentation fact metadata while using existing collector
telemetry. Future archive collectors that add workers or runtime stages need
their own bounded extraction signals.

Collector Performance Evidence: `go test ./internal/collector/archivepreflight -count=1`
proves archive classification is bounded by source bytes, expanded bytes,
entry count, and compression ratio. `go test ./internal/collector -run
'ZIPArchive|ArchiveRouting' -count=1` proves `.zip` documentation packets
route through preflight while `.tar` and `.tar.gz` stay outside documentation
extraction.

Collector Observability Evidence: this package emits no facts, metrics, spans,
logs, status rows, or graph writes. Git ZIP packet extraction stays inside the
existing `collector.observe`, `collector.stream`, `fact.emit`, and fact-count
signals while returning warning classes through fact readback.

Collector Deployment Evidence: no Docker Compose, Helm, Service, ServiceMonitor,
collector binary, runtime flag, or environment-variable path changes in this
slice. The collector routing test keeps tar formats out of hosted
documentation ingestion while allowing reviewed ZIP documentation packets.

No-Observability-Change: this package is a pure metadata preflight helper. It
adds no worker, queue consumer, graph write, database query, HTTP handler, MCP
tool, runtime stage, or hosted collector path.

## Gotchas / invariants

- Preflight never stores raw archive member paths or source names in `Result`.
- Archive member paths must be local slash-separated names with no drive prefix,
  absolute path, backslash, `..` segment, empty component, or NUL byte.
- Symlinks, hardlinks, device nodes, FIFOs, and unknown tar entry types fail
  closed with skip warning classes.
- Nested archives and credential-like member names are counted and classified,
  not inspected or extracted.
- ZIP compression-ratio checks ignore very small members to avoid noisy header
  ratios; expanded-byte and entry-count budgets still bound the archive.
- A safe preflight result is not approval to ingest. It only means this guard
  did not find a disqualifying package-level issue.

## Related docs

- `docs/internal/design/1738-office-spreadsheet-deck-archive-ingestion.md`
- `docs/public/reference/local-testing.md`
- Go `archive/zip`: https://pkg.go.dev/archive/zip
- Go `archive/tar`: https://pkg.go.dev/archive/tar
- Go `compress/gzip`: https://pkg.go.dev/compress/gzip
