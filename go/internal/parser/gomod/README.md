# Go Module Parser

## Purpose

`internal/parser/gomod` parses Go module manifests (`go.mod`),
module-checksum files (`go.sum`), and workspace files (`go.work`) into the
repository dependency rows the
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
- `go.work` parsing via `modfile.ParseWork` (issue
  [#8823](https://github.com/eshu-hq/eshu/issues/8823)): one
  `config_kind=workspace_use` row per `use` entry, section `use`. `value` is
  the raw path, untrimmed. `name` is the raw path too, except that a path
  blank after trimming (`use ""`, which the go command resolves to the
  workspace root) is named by its Go-quoted spelling, because the content
  writers reject a blank entity name. Every entry counts in `use_count`.
  `godebug` directives are accepted and not recorded, as in go.mod.
  Each workspace `replace` becomes one `config_kind=dependency_replace` row,
  the same shape go.mod uses. `gomod_state` carries `go_version`,
  `toolchain`, `use_count`, `replace_count`, and `replaced_modules`, and no
  `module_path`, so the code-call reducer never treats a workspace root as a
  module root. A workspace emits no `config_kind=dependency` rows.
- Malformed `go.mod` and `go.work` handling: the parser never panics, never invents
  installed-version evidence, and surfaces a `gomod_state.parse_error`
  envelope on the payload so operators can diagnose.

This package does not own parser dispatch (the parent registry routes
`go.mod`, `go.sum`, and `go.work` here by exact filename), repository discovery, fact
persistence, graph projection, advisory range matching, or runtime
reachability scoring.

## Exported surface

The godoc contract is in `doc.go`. Current exports are:

- `Parse(path, isDependency, options)` returns one parser payload for a
  `go.mod`, `go.sum`, or `go.work` file. Other filenames, including
  `go.work.sum`, return an error so the parent
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
operator-facing diagnostic for go.mod and go.work input issues.

## Performance

No-Regression Evidence: `go.work` support (#8823) adds one exact file name to
the registry map, so discovery and `LookupByPath` keep the same O(1) exact-name
lookup. Parsing runs only for files named `go.work`, reads that one file, and
does one `modfile.ParseWork` pass linear in the file size, with no shared state
or locks. Before this change no tracked fixture or golden-corpus repository
contained a `go.work`, so their facts do not move; the one `go.work` added to
the git collector's content-version fixture moves its pinned digest
(`go/internal/collector/repo/git/testdata/content_version_pin.json`) to
`git-collector.v9`. A real repository that has a `go.work` gains a
file fact plus its `workspace_use` and `dependency_replace` Variable entities.
The parser registry fingerprint hashes exact file names, and the git
freshness hint folds it in (#7986), so every repository stores one new
generation on its next sync. `go test -race ./internal/parser/...
./internal/collector/repo/...` passes.

No-Observability-Change: this adds no metric, span, or log. The
`gomod_state.state` and `gomod_state.parse_error` envelope remains the
operator-facing diagnostic for a malformed `go.work`, as it is for `go.mod`.
