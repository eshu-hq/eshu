# internal/exports

Bounded, deterministic, redacted exports of Eshu vulnerability and dependency
evidence into standard wire formats.

This package owns the **serialization seam** for security/supply-chain
exports. It does not read the database, does not decide authorization, and
does not expand scope. Callers (HTTP handlers, MCP tools, the `eshu` CLI)
gather already-authorized evidence into a [`Snapshot`](exports.go), then ask
a [`Registry`](registry.go) for an [`Exporter`](registry.go) and write bytes
to a `io.Writer`.

## Why this package exists

Operators want SARIF, CycloneDX BOV, SPDX, and GitHub dependency snapshots so
their existing security tooling can ingest Eshu evidence without scraping the
API/MCP text responses. Building one writer per call site would duplicate the
scope/redaction rules, leak format-specific knowledge into handlers, and make
each format diverge from the next over time. This package centralizes:

- **Bounded scope.** A [`Scope`](exports.go) value declares one target
  (repository, image digest, package, or advisory). Exporters drop findings
  and components whose own scope identifiers disagree with the snapshot
  scope. A handler bug that mixes a second target's evidence into the input
  cannot leak through the writer.
- **Scanner status.** [`SnapshotStatus`](exports.go) carries optional
  readiness, missing-evidence, unsupported-target, and exit-code context for
  scanner-style exports. SARIF uses this to avoid turning
  `evidence_incomplete` or `unsupported` scans into empty clean results.
- **Deterministic output.** Findings, components, advisory sources, and
  locations are sorted before serialization. The same input snapshot produces
  byte-identical output across processes. Golden fixtures under
  [`testdata/sarif/`](testdata/sarif) lock the contract.
- **Redaction.** An optional [`FieldRedactor`](exports.go) rewrites manifest
  paths and locator URIs before they leave the process. This is defense in
  depth against private repo paths or internal file layout escaping in
  `physicalLocation.artifactLocation.uri`.

## Supported formats

| Format | Constant | Status |
| --- | --- | --- |
| SARIF v2.1.0 | `FormatSARIF` | Implemented today |
| CycloneDX BOV (vulnerability-enriched SBOM) | `FormatCycloneDXBOV` | Reserved; exporter ships in a follow-up |
| SPDX 2.3 | `FormatSPDX` | Reserved; exporter ships in a follow-up |
| GitHub dependency snapshot | `FormatGitHubDependencySnapshot` | Reserved; exporter ships in a follow-up |

`Registry.Export` returns `ErrUnsupportedFormat` for reserved formats so
callers can surface a clean `unsupported_capability` response while the
exporter is still being built.

## What goes where

| Concern | File |
| --- | --- |
| Format-neutral types (`Scope`, `Finding`, `Component`, `Snapshot`, `SnapshotStatus`, `Options`, `Tool`, `FieldRedactor`) | [`exports.go`](exports.go) |
| Registry + format extension surface | [`registry.go`](registry.go) |
| SARIF v2.1.0 writer | [`sarif.go`](sarif.go) |
| Unit tests for scope/severity/registry | [`exports_test.go`](exports_test.go) |
| SARIF golden fixture tests | [`sarif_test.go`](sarif_test.go) |
| Golden fixtures | [`testdata/sarif/`](testdata/sarif) |

## Adding a new format

1. Define the writer in a new file (e.g. `cyclonedx.go`).
2. Implement [`Exporter`](registry.go): `Format()` returns the new
   `Format` constant; `Export()` re-validates `snapshot.Scope`, filters
   findings/components against the scope, applies `opts.Redactor` to any
   path-like fields, sorts deterministically, and writes the bytes.
3. Register the exporter inside [`NewRegistry`](registry.go) so every caller
   picks it up.
4. Ship at least three golden fixtures: an empty scope, a representative
   single-evidence case, and a multi-evidence case that exercises
   scope-drop + redaction. Mirror the layout under
   `testdata/<format-slug>/`.
5. Update [`AGENTS.md`](AGENTS.md) with the new format invariants.

## Determinism contract

The writer surface is byte-stable across:

- Map iteration order (every map serialized to JSON is sorted before emit).
- Input ordering (findings, locations, advisory sources sorted explicitly).
- Goroutine interleaving (exporters are stateless; no shared state).
- Time (only `snapshot.GeneratedAt` is serialized; the writer never reads
  wall-clock time).
- Formatting (two-space indent and a single trailing newline produced by
  `encoding/json.Encoder.Encode`).

The golden fixture tests compare with `bytes.Equal` against the on-disk
fixture so a formatting change (indentation, key escaping, trailing
newline) fails the test even if the JSON value is semantically identical.
`TestSARIFExporter_IsDeterministic` runs the writer twice and asserts
`bytes.Equal`. Add the same test shape for each new format.

SARIF emits scanner status in `run.properties` with `eshu.` keys. For
non-ready scanner states such as `evidence_incomplete` or `unsupported`, the
writer also emits one location-free status result so code-scanning consumers
do not interpret an empty vulnerability finding list as a clean scan. The
result intentionally omits `locations`; callers must not invent source paths
for missing evidence.

## Redaction contract

If `opts.Redactor` is non-nil, every `Location.ManifestPath` value goes
through `RedactPath` before serialization. Implementations should be
deterministic, return a non-empty marker for redacted paths, and never
return raw secret material. A nil redactor preserves paths verbatim and is
appropriate only when the caller has already proven that every path in the
snapshot belongs to the requested scope.

The redactor is applied to a **deep copy** of the locations slice so the
caller's snapshot is never mutated.

## Telemetry

This package emits no logs, metrics, or spans. Callers own observability:
wrap `Registry.Export` in a span and record an
`eshu_dp_export_total{format,scope_kind}` counter and an
`eshu_dp_export_duration_seconds` histogram. The package surface keeps
collector-neutrality so it remains usable from CLI, MCP, and HTTP
contexts without one of those carrying the others' instrumentation.

## Verification

```bash
cd go && go test ./internal/exports/... -count=1
cd go && golangci-lint run ./internal/exports/...
```

Regenerate golden fixtures when intentional output changes land:

```bash
cd go && go test ./internal/exports/... -run TestSARIFExporter_GoldenFixtures -update-golden -count=1
```

Inspect the diff carefully; a regenerated fixture is a wire contract
change.
