# Go Module Parser

## Purpose

`internal/parser/gomod` parses Go module manifests (`go.mod`) and
module-checksum files (`go.sum`) into the repository dependency rows the
supply-chain consumption reducer can join to package-registry identity. The
parser is the Git side of the Go ecosystem dependency evidence story for
issue [#643](https://github.com/eshu-hq/eshu/issues/643): without it,
Go repositories appear as `missing_evidence: ["owned_packages"]` on the
supply-chain impact readiness envelope even when registry data is present.

## Ownership boundary

This package owns:

- `go.mod` parsing via `golang.org/x/mod/modfile` (the official upstream
  parser), including `require`, `require ... // indirect`, `replace`,
  `exclude`, and `retract` directives.
- Replacement resolution that joins the matching `replace` directive into
  the originating require row, surfaces a `resolved_module_path` /
  `resolved_version` pair, and emits a standalone `replace` row with
  `config_kind=dependency_replace` so the source intent stays auditable
  without being admitted as consumption.
- `go.sum` parsing into `config_kind=dependency_checksum` rows tagged
  `ambiguous=true`. The reducer never admits these as consumption because
  `go.sum` records every module version any tool has verified, not the
  currently selected version.
- Malformed `go.mod` handling: the parser never panics, never invents
  installed-version evidence, and surfaces a `gomod_state.parse_error`
  envelope on the payload so operators can diagnose.

This package does not own parser dispatch (the parent registry routes
`go.mod` and `go.sum` here by exact filename), repository discovery, fact
persistence, graph projection, advisory range matching, or runtime
reachability scoring.

## Exported surface

The godoc contract is in `doc.go`. Current exports are:

- `Parse(path, isDependency, options)` returns one parser payload for a
  `go.mod` or `go.sum` file. Other filenames return an error so the parent
  engine never silently falls back to raw-text mode for an unsupported
  go-module file.
- `LanguageName` and `PackageManager` are the canonical string constants
  stamped on every emitted row so reducer wiring and tests stay in
  lockstep.

## Dependencies

This package imports `internal/parser/shared` for `Options`, `BasePayload`,
and `ReadSource`, and `golang.org/x/mod/modfile` for the upstream go.mod
syntax tree. It must not import `internal/parser`, collector, storage,
query, projector, or reducer packages.

## Telemetry

This package emits no metrics, spans, or logs. Parser timing and failures
remain owned by the collector snapshot path and the parent engine; the
malformed-state envelope on the payload (`gomod_state.parse_error`) is the
operator-facing diagnostic for go.mod input issues.
