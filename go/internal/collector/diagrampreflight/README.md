# Diagram Preflight

## Purpose

`collector/diagrampreflight` classifies diagram documentation sources before
any diagram extractor reads labels, links, or diagram text. It gives future
documentation collectors a metadata-only guard for resource limits, malformed
structured input, external references, include directives, active content, and
sensitive-looking markers.

## Ownership boundary

This package owns pure preflight classification for `.svg`, `.drawio`,
`.excalidraw`, `.mmd`, `.mermaid`, `.puml`, `.plantuml`, and `.d2` files. It
reads bounded source bytes only to classify metadata and warning counts. It
does not persist diagram text, labels, URLs, source names, included paths, or
private identifiers.

It does not discover repositories, emit documentation facts, persist rows, write
graph state, expose API or MCP routes, add runtime knobs, execute renderers, run
preprocessors, follow includes, fetch network resources, or enable hosted or
repository ingestion. Diagram extraction, fact emission, ACL behavior, and
security-review enablement belong in separate follow-up slices.

## Exported surface

See `doc.go` for the godoc-rendered package contract.

- `Options` sets source-byte, element-count, and XML/JSON-depth budgets.
- `Result` reports format, safe/unsafe state, bounded counts, and warning
  classes.
- `Warning` records a stable low-cardinality class and count.
- `Preflight` classifies one diagram using an `io.ReaderAt`, source name, byte
  size, context, and options.
- Format constants cover SVG, draw.io, Excalidraw, Mermaid, PlantUML, and D2.
- Warning constants cover unsupported formats, malformed XML/JSON, resource
  limits, timeout, remote includes, active content, external references, and
  sensitive-value redaction.

## Dependencies

The package uses only the Go standard library. `encoding/xml` parses bounded
SVG and draw.io metadata, `encoding/json` parses bounded Excalidraw metadata,
and `bufio.Scanner` inspects text-diagram lines without storing source text.

## Telemetry

None directly. This package has no runtime side effects and no metric, log, or
trace emission. Future diagram collector integration must record bounded
extraction attempts, warning classes, skipped references, bytes inspected,
elapsed time, and resource outcomes through collector telemetry before enabling
diagram ingestion.

Collector Performance Evidence: `go test ./internal/collector/diagrampreflight
-count=1` proves diagram classification is bounded by source bytes, element
count, and structured-input depth. `go test ./internal/collector -run
'Diagram|DocumentationDefaultOff' -count=1` proves diagram formats remain
outside documentation extraction by default.

Collector Observability Evidence: this package emits no facts, metrics, spans,
logs, status rows, or graph writes. Future extractor wiring must add runtime
collector signals for attempted diagram preflights, warning classes, skipped
references, elapsed time, bytes inspected, and resource-limit outcomes before
enabling diagram ingestion.

Collector Deployment Evidence: no Docker Compose, Helm, Service, ServiceMonitor,
collector binary, runtime flag, or environment-variable path changes in this
slice. The default-off collector routing test keeps diagram sources out of
hosted documentation ingestion until a reviewed extractor slice enables them.

No-Observability-Change: this package is a pure metadata preflight helper. It
adds no worker, queue consumer, graph write, database query, HTTP handler, MCP
tool, runtime stage, or hosted collector path.

## Gotchas / invariants

- Preflight never stores raw diagram text, labels, URLs, source names, or include
  paths in `Result`.
- Active SVG/script/event-handler and JavaScript references fail closed with
  `unsupported_active_content`.
- Include directives, XML entities, and external resource references are counted
  and classified, not followed.
- A safe preflight result is not approval to ingest. It only means this guard
  did not find a disqualifying package-level issue.

## Related docs

- `docs/internal/design/1737-visual-media-documentation-ingestion.md`
- `docs/public/reference/local-testing.md`
