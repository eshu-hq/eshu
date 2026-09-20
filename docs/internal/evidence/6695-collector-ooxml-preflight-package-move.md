# #6695 collector ooxml preflight package move

## Scope

Base: `origin/main` at `3f74d5050`.

This slice moves the OOXML safety-classifier package from the historical
flat path `go/internal/collector/ooxmlpreflight` to
`go/internal/collector/preflight/ooxml`. The package name changes to
`ooxml`; `Options`, `Result`, `Warning`, `Preflight`, the structure
classifier, and the format and warning constants remain exact. This is the
sixth `preflight/` leaf (the parent trio and the `archive`, `pdf`,
`diagram`, `picture`, and `media` leaves landed earlier); the manifest
sibling stays flat for a later slice.

Filenames are unchanged (`preflight.go` and `structure.go` do not repeat
their own `ooxml` directory, per the #6627 `cicd/run` precedent which kept
`planner.go`). The `ooxml` name does not shadow the standard library, so
no import alias is needed anywhere. The `gitdocs` caller imports the new
path without an alias, so its selectors read `ooxml.*` per naming.md
rule 5 (no glued name carried into the new home).

The `go/internal/collector/preflight` parent stays a documentation-only
namespace: this slice only adds the `ooxml` child to its prose. The
`ooxml` leaf owns metadata-only classification of `.docx`, `.xlsx`, and
`.pptx` sources. It is not an independently deployable service.
Extraction, fact emission, ACL handling, security review, and telemetry
stay in the owning collector slice.

## Inventory and dependency edges

At the base, the leaf contained exactly six files: `AGENTS.md`,
`README.md`, `doc.go`, `preflight.go`, `structure.go`, and
`preflight_test.go` (three non-test Go files, all `package
ooxmlpreflight`). The destination contains those same six file names as
`package ooxml`. No new parent trio:
`preflight/{doc.go,README.md,AGENTS.md}` already exists.

The destination leaf's production imports remain standard-library only,
unaliased. It imports neither the collector root nor a sibling collector.
No dirgate ledger row changes: nothing pinned (`awscloud`, `gcpcloud`,
`gitrepo`) is touched, and no collector-root file repeats the new
`preflight` parent name, so no naming finding is introduced.

The single production caller is `gitdocs` (`git_documentation_ooxml.go`,
`git_documentation_docx.go`, `git_documentation_pptx.go`,
`git_documentation_xlsx.go` plus one test file): import repoint plus
qualifier rename only.

## Test discovery

`go test -list '.*'` on the moved package (proves the move carried the
tests, per `golang-engineering`); first 10 of the list:

```text
go test -list '.*' ./internal/collector/preflight/ooxml/
TestPreflightAcceptsNormalOOXMLPackageMetadata
TestPreflightRecognizesSupportedOOXMLFormats
TestPreflightRejectsMacroEnabledOfficeExtensions
TestPreflightFlagsUnsafePathsExternalRelationshipsAndActiveParts
TestPreflightFlagsExternalRelationshipTargetWithoutMode
TestPreflightClassifiesMalformedContainerAndXML
TestPreflightClassifiesMalformedStructureXML
TestPreflightClassifiesResourceLimits
TestPreflightSkipsOversizedStructureXML
TestPreflightClassifiesXMLDepthLimit
```

All run green with `-count=1`, as do the `gitdocs` tests that cover the
repointed importer (including
`TestOOXMLPreflightBlocksExtractionOnlyForFatalWarnings`, confirmed by
`-list`).

## Preserved contracts

- `Preflight`, structure classification, `Options` normalization, warning
  classes, counts, JSON shape, resource budgets, and error behavior are
  unchanged.
- `gitdocs` selectors are renamed `ooxmlpreflight.*` to `ooxml.*`
  (same identifiers otherwise); only the import lines and the qualifier
  changed (plus gofmt import ordering).
- The moved README's evidence commands are corrected since those lines
  were already in this diff: the first names the new leaf path, and the
  second names the `gitdocs` package where the routing test lives
  (`TestOOXMLPreflightBlocksExtractionOnlyForFatalWarnings`, confirmed by
  `-list`; a `go test ./internal/collector -run ...` form selects zero
  tests).
- No wire, API, fact, reducer, query, graph, storage, queue, lease,
  concurrency, or runtime behavior changes.

## Verification

All Go commands ran from `go/` with `env -u GOROOT`, a worktree-local
`GOCACHE`, and `GOTMPDIR=/tmp/e6695ooxml`, serially:

- `go build ./internal/collector/preflight/ooxml/ ./internal/collector/gitrepo/gitdocs/`:
  exit 0.
- `go vet ./internal/collector/preflight/ooxml/ ./internal/collector/gitrepo/gitdocs/`:
  exit 0.
- `go test -count=1 ./internal/collector/preflight/ooxml/ ./internal/collector/gitrepo/gitdocs/`:
  exit 0 (`preflight/ooxml` ok, `gitdocs` ok).
- `gofmt -l` on both touched trees: clean.
- `scripts/verify-package-docs.sh`, `scripts/verify-telemetry-coverage.sh`,
  `scripts/verify-moved-file-refs.sh` (4 vacated Go paths, no dangling refs),
  `scripts/verify-filename-stutter.sh`,
  `scripts/verify-doc-citations.sh`: all pass on the committed HEAD.

Deliberately not run (orchestrator promotion gates): `make pre-pr`,
`make pre-pr-full`. Promotion ran `eshu-code-review` (self-review, READY,
P0/P1/P2-blocking 0), `review-attest` capture/verify, `make pre-push`
(all local gates passed), and the strict docs build
(`mkdocs build --strict --clean`).

## No-Regression Evidence (#6695):

- Baseline: branch base `origin/main` at `3f74d5050`, measured in a
  throwaway worktree (`/tmp/ooxml-base`, removed after the run) on the
  old path `go/internal/collector/ooxmlpreflight`.
- After: branch `feat/6695-ooxml-leaf` at the refactor commit,
  new path `go/internal/collector/preflight/ooxml`.
- Backend/version: no live backend exercised. Unit tests only against the
  in-repo classifier; no NornicDB, Postgres, or Docker in these packages.
  Toolchain `go1.27.1 darwin/arm64` both sides, same machine, serial runs.
  Wall times are same-machine relative readings, not reference targets.
- Input shape: `go test -count=1` on the old path (base) and the new path
  (branch); `go test -list '.*'` for test discovery.
- Baseline measurement: `ooxmlpreflight` ok 0.407s, `gitdocs`
  ok 0.559s.
- After measurement: `preflight/ooxml` ok 0.171s;
  importer `gitdocs` ok 0.225s.
- Terminal counts: 2/2 test-bearing touched packages green, zero failures.
- Query/concurrency proof: the moved `preflight.go` and `structure.go`
  pair at 99% similarity (package-clause delta only); the added-line scan
  of the Go diff finds no Cypher/SQL keywords, no telemetry identifiers,
  and no new goroutine or error-path lines — logic is byte-identical to
  base.
- Telemetry/log/status evidence: added-line identifier scan
  (metric/counter/histogram/gauge/tracer/otel/prometheus/pprof) returns
  empty; `go vet` clean on the touched tree.
- Why the change is safe: rename-only nest with one mechanical importer
  repoint (compiler-checked by the collector-tree build); the relocated
  tests pass in the baseline time band.

## No-Observability-Change (#6695):

- The `telemetry-coverage.md` row for OOXML preflight names the new
  `preflight/ooxml/*.go` path with the same `No-Observability-Change`
  reason: git-source collector metrics cover OOXML preflight throughput.
- `scripts/verify-telemetry-coverage.sh` agrees: no new untracked stages.
