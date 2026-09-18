# #6695 collector archive preflight package move

## Scope

Base: `origin/main` at `3ff493935` (post-#6758).

This slice moves the archive safety-classifier package from the historical
flat path `go/internal/collector/archivepreflight` to
`go/internal/collector/preflight/archive`. The package name changes to
`archive`; `Options`, `Result`, `Warning`, `Preflight`, and the format and
warning constants remain exact. This is the first `preflight/` leaf; the
diagram, image, media, ooxml, pdf, and manifest siblings stay flat for
later slices.

Filenames are unchanged (`preflight.go` does not repeat its own `archive`
directory, per the #6627 `cicd/run` precedent which kept `planner.go`).
The three gitdocs callers import the new path without an alias, so their
selectors read `archive.*` per naming.md rule 5 (no glued name carried
into the new home).

The new `go/internal/collector/preflight` package is a documentation-only
namespace. It adds no runtime declaration, import, or initialization. The
`archive` leaf owns metadata-only classification of `.zip`, `.tar`,
`.tar.gz`, and `.tgz` packages. It is not an independently deployable
service. Extraction, fact emission, ACL handling, security review, and
telemetry stay in the owning collector slice.

## Inventory and dependency edges

At the base, the leaf contained exactly six files: `AGENTS.md`, `README.md`,
`doc.go`, `preflight.go`, `preflight_test.go`, and `targz_test.go` (two
non-test Go files). The destination contains those same six file names.
The new namespace parent contains exactly three direct files: `AGENTS.md`,
`README.md`, and declaration-only `doc.go`.

The destination leaf's production imports remain standard-library only. It
imports neither the collector root nor a sibling collector. No dirgate
ledger row changes: nothing pinned (`awscloud`, `gcpcloud`, `gitrepo`) is
touched, and no collector-root file repeats the new `preflight` parent
name, so no naming finding is introduced.

The three reverse importers are unchanged after substituting the package
path:

- `go/internal/collector/gitrepo/gitdocs/git_documentation_archive.go`
- `go/internal/collector/gitrepo/gitdocs/git_documentation_archive_tar.go`
- `go/internal/collector/gitrepo/gitdocs/git_documentation_archive_helpers.go`

No forwarding package or duplicate classifier remains.

## TDD evidence

Rename-only move; no behavior touch. The repoint proof is test discovery
at the new path (a stale `-run` pattern selects zero tests and still
exits 0):

```text
go test -list '.*' ./internal/collector/preflight/archive/
TestPreflightSafeZipAndTarMetadata
TestPreflightClassifiesUnsupportedAndMalformedContainers
TestPreflightClassifiesResourceLimits
TestPreflightClassifiesCompressionRatioLimit
TestPreflightClassifiesUnsafeMemberMetadata
TestPreflightClassifiesCanceledContextAsTimeout
TestPreflightResultJSONOmitsSourceAndMemberNames
TestPreflightSafeTarGzipMetadata
TestPreflightClassifiesMalformedTarGzip
TestPreflightTarGzipResourceAndWarningClasses
TestPreflightTarGzipCanceledContextAsTimeout
TestPreflightTarGzipResultJSONOmitsSourceAndMemberNames
```

All twelve run green with `-count=1`, as do the six routing tests in the
owning `gitrepo` package (`-run 'ZIPArchive|TARArchive|ArchiveRouting'`).

## Preserved contracts

- `Preflight`, `Options` normalization, warning classes, counts, JSON
  shape, resource budgets, and error behavior are unchanged.
- `gitdocs` selectors are renamed `archivepreflight.*` to `archive.*`
  (same identifiers otherwise); only the import lines and the qualifier
  changed (plus gofmt import ordering in one file).
- The moved README's second evidence command named
  `go test ./internal/collector -run ...`, which selects zero tests: the
  routing tests live in the `gitrepo` package. That package path is
  corrected in the moved README since the line was already in this diff.
- No wire, API, fact, reducer, query, graph, storage, queue, lease,
  concurrency, or runtime behavior changes.

## Verification

All Go commands ran from `go/` with `env -u GOROOT`, a worktree-local
`GOCACHE`, and `GOTMPDIR=/tmp/e6695`, serially:

- `go build ./...`: exit 0.
- `go vet ./internal/collector/preflight/... ./internal/collector/gitrepo/...`:
  exit 0.
- `go test -count=1 ./internal/collector/preflight/... ./internal/collector/gitrepo/...`:
  exit 0.
- `go test -count=1 ./internal/collector/gitrepo/ -run 'ZIPArchive|TARArchive|ArchiveRouting'`:
  exit 0 (6 tests selected, confirmed via `-list`).
- `gofmt -l` on both touched trees: clean.
- `scripts/verify-package-docs.sh`, `scripts/verify-telemetry-coverage.sh`,
  `scripts/verify-moved-file-refs.sh`, `scripts/verify-dirgate.sh`: exit 0.

Deliberately not run (orchestrator promotion gates): `make pre-push`,
`make pre-pr`, `make pre-pr-full`, `review-attest`, `eshu-code-review`.

## No-Regression Evidence (#6695):

- Baseline: `origin/main` at `3ff493935`; the old-path package is
  byte-identical between `f89b05014` and `3ff493935`, so the timing below
  (measured on a clean main checkout) is a valid baseline for both.
- After: branch `feat/6695-preflight-archive` post-rebase.
- Backend/version: no live backend exercised. Unit tests only against the
  in-repo classifiers; no NornicDB, Postgres, or Docker in these packages.
  Toolchain `go1.27.1 darwin/arm64` both sides, same machine, serial runs.
  Wall times are same-machine relative readings, not reference targets.
- Input shape: `go test -count=1` on the old path (base) and the new path
  (branch); `go test -list '.*'` for test discovery.
- Baseline measurement: `archivepreflight` ok 0.475s (12 tests).
- After measurement: `preflight/archive` ok 0.301s (same 12 tests,
  confirmed by name via `-list`); `gitrepo` ok 10.720s, `gitdocs` ok
  0.764s including the 6 archive-routing tests.
- Terminal counts: 3/3 test-bearing touched packages green, zero failures.
- Query/concurrency proof: the moved `preflight.go` pairs at R099
  (package-clause-only delta); added-line scan of the Go diff finds no
  Cypher/SQL keywords, no telemetry identifiers, and no new goroutine or
  error-path lines — logic is byte-identical to base.
- Telemetry/log/status evidence: added-line identifier scan
  (metric/counter/histogram/gauge/tracer/otel/prometheus/pprof) returns
  empty; `go vet` clean on both touched trees.
- Why the change is safe: rename-only nest with compiler-checked importer
  repoints (explicit alias keeps selectors byte-identical); the relocated
  tests pass in the baseline time band.

## No-Observability-Change (#6695):

No operator-facing signal changes. No metrics, spans, structured-log
fields, status endpoints, or pprof knobs are added or altered — the
added-line telemetry scan above is empty, and warning-class strings
(`unsupported_format`, `malformed_container`, ...) are unchanged values,
not new signals.
