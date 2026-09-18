# #6695 collector diagram preflight package move

## Scope

Base: `origin/main` at `8cd8866c7`.

This slice moves the diagram safety-classifier package from the historical
flat path `go/internal/collector/diagrampreflight` to
`go/internal/collector/preflight/diagram`. The package name changes to
`diagram`; `Options`, `Result`, `Warning`, `Preflight`, and the format and
warning constants remain exact. This is the second `preflight/` leaf (the
parent trio and the `archive` leaf landed earlier); the image, media, ooxml,
pdf, and manifest siblings stay flat for later slices.

Filenames are unchanged (`preflight.go` does not repeat its own `diagram`
directory, per the #6627 `cicd/run` precedent which kept `planner.go`).
The single gitdocs caller imports the new path without an alias, so its
selectors read `diagram.*` per naming.md rule 5 (no glued name carried
into the new home).

The `go/internal/collector/preflight` parent stays a documentation-only
namespace: this slice only adds the `diagram` child to its prose. The
`diagram` leaf owns metadata-only classification of `.svg`, `.drawio`,
`.excalidraw`, `.mmd`, `.mermaid`, `.puml`, `.plantuml`, and `.d2` sources.
It is not an independently deployable service. Extraction, fact emission,
ACL handling, security review, and telemetry stay in the owning collector
slice.

## Inventory and dependency edges

At the base, the leaf contained exactly five files: `AGENTS.md`,
`README.md`, `doc.go`, `preflight.go`, and `preflight_test.go` (two
non-test Go files, all `package diagrampreflight`). The destination
contains those same five file names as `package diagram`. No new parent
trio: `preflight/{doc.go,README.md,AGENTS.md}` already exists.

The destination leaf's production imports remain standard-library only. It
imports neither the collector root nor a sibling collector. No dirgate
ledger row changes: nothing pinned (`awscloud`, `gcpcloud`, `gitrepo`) is
touched, and no collector-root file repeats the new `preflight` parent
name, so no naming finding is introduced.

The single reverse importer is unchanged after substituting the package
path:

- `go/internal/collector/gitrepo/gitdocs/git_documentation_diagram.go`

No forwarding package or duplicate classifier remains. No refs exist in
`go/.golangci.yml`, `specs/`, `scripts/`, `.github/`, or the capability
catalog, so no surface-inventory, replay, or lint lockstep applies beyond
the telemetry-coverage Diagram row and the leaf self-path docs updated
here.

## TDD evidence

Rename-only move; no behavior touch. The repoint proof is test discovery
at the new path (a stale `-run` pattern selects zero tests and still
exits 0):

```text
go test -list '.*' ./internal/collector/preflight/diagram/
TestPreflightAcceptsSafeDiagramMetadata
TestPreflightClassifiesUnsupportedAndMalformed
TestPreflightClassifiesResourceLimits
TestPreflightClassifiesUnsafeReferencesAndContent
TestPreflightClassifiesCanceledContextAsTimeout
TestPreflightResultJSONOmitsSourceAndDiagramText
```

All six run green with `-count=1`, as do the eight routing tests in the
owning `gitrepo` package (`-run 'Diagram|DocumentationDefaultOff'`,
confirmed by name via `-list`, 8/8 PASS).

## Preserved contracts

- `Preflight`, `Options` normalization, warning classes, counts, JSON
  shape, resource budgets, and error behavior are unchanged.
- `gitdocs` selectors are renamed `diagrampreflight.*` to `diagram.*`
  (same identifiers otherwise); only the import lines and the qualifier
  changed (plus gofmt import ordering).
- The moved README's evidence commands are corrected since those lines
  were already in this diff: the first names the new leaf path, and the
  second names the `gitrepo` package where the diagram routing tests live
  (a `go test ./internal/collector -run ...` form selects zero tests).
- No wire, API, fact, reducer, query, graph, storage, queue, lease,
  concurrency, or runtime behavior changes.

## Verification

All Go commands ran from `go/` with `env -u GOROOT`, a worktree-local
`GOCACHE`, and `GOTMPDIR=/tmp/e6695fourth`, serially:

- `go build ./internal/collector/preflight/... ./internal/collector/gitrepo/...`:
  exit 0 (16.9s real, cold cache).
- `go vet ./internal/collector/preflight/... ./internal/collector/gitrepo/...`:
  exit 0 (3.4s real).
- `go test -count=1 ./internal/collector/preflight/... ./internal/collector/gitrepo/...`:
  exit 0 (`preflight/archive` ok 2.393s, `preflight/diagram` ok 2.390s,
  `gitrepo` ok 10.943s, `gitdocs` ok 1.795s).
- `go test -count=1 ./internal/collector/gitrepo/ -run 'Diagram|DocumentationDefaultOff'`:
  exit 0 (8 tests selected, confirmed via `-list`, 8/8 PASS).
- `gofmt -l` on both touched trees: clean.
- `scripts/verify-package-docs.sh`, `scripts/verify-telemetry-coverage.sh`,
  `scripts/verify-moved-file-refs.sh` (3 vacated paths, no dangling refs),
  `scripts/verify-filename-stutter.sh`,
  `scripts/verify-doc-citations.sh`: all pass on the committed HEAD.

Deliberately not run (orchestrator promotion gates): `make pre-pr`,
`make pre-pr-full`. Promotion ran `eshu-code-review` (self-review, READY,
P0/P1/P2-blocking 0), `review-attest` capture/verify, and `make pre-push`.

## No-Regression Evidence (#6695):

- Baseline: branch base `origin/main` at `c2ed7585d`, measured in a
  throwaway worktree (`/tmp/diag-base`, removed after the run) on the
  old path `go/internal/collector/diagrampreflight`.
- After: branch `feat/6695-preflight-diagram` at the refactor commit,
  new path `go/internal/collector/preflight/diagram`.
- Backend/version: no live backend exercised. Unit tests only against the
  in-repo classifier; no NornicDB, Postgres, or Docker in these packages.
  Toolchain `go1.27.1 darwin/arm64` both sides, same machine, serial runs.
  Wall times are same-machine relative readings, not reference targets.
- Input shape: `go test -count=1` on the old path (base) and the new path
  (branch); `go test -list '.*'` for test discovery.
- Baseline measurement: `diagrampreflight` ok 0.758s (6 tests).
- After measurement: `preflight/diagram` ok 0.328s (same 6 tests,
  confirmed by name via `-list`); untouched siblings `preflight/archive`
  ok 0.192s and `preflight/pdf` ok 0.454s; importer `gitdocs` ok 0.768s.
- Terminal counts: 2/2 test-bearing touched packages green, zero failures.
- Query/concurrency proof: the moved `preflight.go` pairs at 99%
  similarity (package-clause-only delta); the added-line scan of the Go
  diff finds no Cypher/SQL keywords, no telemetry identifiers, and no new
  goroutine or error-path lines — logic is byte-identical to base.
- Telemetry/log/status evidence: added-line identifier scan
  (metric/counter/histogram/gauge/tracer/otel/prometheus/pprof) returns
  empty; `go vet` clean on the touched tree.
- Why the change is safe: rename-only nest with one mechanical importer
  repoint (compiler-checked by the collector-tree build); the relocated
  tests pass in the baseline time band.

## No-Observability-Change (#6695):

No operator-facing signal changes. No metrics, spans, structured-log
fields, status endpoints, or pprof knobs are added or altered — the
added-line telemetry scan above is empty, and warning-class strings
(`unsupported_format`, `malformed_xml`, `malformed_json`,
`resource_limit_exceeded`, `timeout`, `unsupported_remote_include`,
`unsupported_active_content`, ...) are unchanged values, not new signals.
