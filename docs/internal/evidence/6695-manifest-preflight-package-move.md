# #6695 manifest leaf: exportmanifestpreflight → preflight/manifest move evidence

Rename-only nest per #6695 (`collector/exportmanifestpreflight` becomes
`collector/preflight/manifest`, package `manifest`, doc trio carried).
`document/export` importers repointed; its local `manifest` struct
renamed to `exportManifest` (package-name collision, no behavior change).
Package identifiers de-stuttered per naming rules 4-5 (`candidate` types,
`classify`/`readBounded`/`unsafePath` helpers, `WarningInvalid` constant).

Companion nest in the same leaf per naming rule 5 (`documentationexport`
is a glued compound touched by the importer repoints): `documentationexport`
becomes `collector/document/export` (package `export`) with a documentation-only
`document/` parent trio, `contract_encode_adoption_test.go` path entry,
telemetry row, replay-coverage markdown/spec plus regenerated
`replay-coverage-report.json`, and design-doc pointers updated. Zero external
importers; no cycle risk (`export` reads `facts`, `scope`, `preflight/manifest`
only). `TestContractEncodeAdoptionRatchet` green against the new path.

No-Regression Evidence: `go test -count=1` on both affected packages,
baseline origin/main `1a3a110d8` vs leaf head, toolchain go1.27.1
darwin/arm64, same input shape (package unit suites, no fixtures or
backend): baseline `exportmanifestpreflight` ok (0.334s) and
`documentationexport` ok (0.462s); after move `preflight/manifest` ok
(0.205s) and `document/export` ok (0.505s). `go test -list` discovers
20 tests on both sides (identical set modulo the package path). `go vet`
clean on both packages recursive. Safe because the move is byte-identical
except the package clause, import path/qualifiers, and renames with no
logic, JSON-tag, or warning-value change (`export_manifest_invalid`
wire value preserved).

No-Observability-Change: the package emits no facts, metrics, spans,
logs, status rows, or graph writes (pure metadata-only classifier); the
telemetry-coverage row path update is a pointer change only, and
`verify-telemetry-coverage.sh` agrees instruments with coverage.
