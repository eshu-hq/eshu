# #6695 collector picture preflight package move

## Scope

Base: `origin/main` at `b7618bb2c`.

This slice moves the image safety-classifier package from the historical
flat path `go/internal/collector/imagepreflight` to
`go/internal/collector/preflight/picture`. The package name changes to
`picture` (not `image`: a review thread caught that `package image` shadows
the standard library wherever both are imported, forcing a `stdimage` alias
in three files; the owner directed a rename of leaf and directory instead,
per the `golang-engineering` stdlib-shadowing rule. No `package picture`
collides with the standard library, and the directory/package names match. `Options`, `Result`, `Warning`, `Preflight`, and the format and
warning constants remain exact. This is the fifth `preflight/` leaf (the
parent trio and the `archive`, `pdf`, `diagram`, and `media` (via #6870)
leaves landed earlier); the ooxml and manifest siblings stay flat for
later slices.

Filenames are unchanged (`preflight.go` does not repeat its own `picture`
directory, per the #6627 `cicd/run` precedent which kept `planner.go`).
The `ocrdoc` caller imports the new path without an alias, so its selectors
read `picture.*` per naming.md rule 5 (no glued name carried into the new
home).

The `go/internal/collector/preflight` parent stays a documentation-only
namespace: this slice only adds the `picture` child to its prose. The
`picture` leaf owns metadata-only classification of `.png`, `.jpeg`, `.jpg`,
and `.gif` sources. It is not an independently deployable service.
Extraction, fact emission, ACL handling, security review, and telemetry
stay in the owning collector slice.

## Inventory and dependency edges

At the base, the leaf contained exactly five files: `AGENTS.md`,
`README.md`, `doc.go`, `preflight.go`, and `preflight_test.go` (two
non-test Go files, all `package imagepreflight`). The destination
contains those same five file names as `package picture`. No new parent
trio: `preflight/{doc.go,README.md,AGENTS.md}` already exists.

The destination leaf's production imports remain standard-library only,
unaliased. It imports neither the collector root nor a sibling collector.
No dirgate ledger row changes: nothing pinned (`awscloud`, `gcpcloud`,
`gitrepo`) is touched, and no collector-root file repeats the new
`preflight` parent name, so no naming finding is introduced.

The single production caller is `ocrdoc` (`extract.go`, `types.go` plus
two test files): import repoint plus qualifier rename only.

## Test discovery

`go test -list '.*'` on the moved package (proves the move carried the
tests, per `golang-engineering`):

```text
go test -list '.*' ./internal/collector/preflight/picture/
TestPreflightAcceptsSafeImageMetadata
TestPreflightClassifiesUnsupportedMalformedAndLimits
TestPreflightClassifiesMetadataSensitiveAndExternalMarkers
TestPreflightClassifiesAnimatedGIFFirstFrameOnly
TestPreflightClassifiesCanceledContextAsTimeout
TestPreflightResultJSONOmitsSourceAndImageContent
```

All six run green with `-count=1`, as do the `ocrdoc` tests that cover
the repointed importer.

## Preserved contracts

- `Preflight`, `Options` normalization, warning classes, counts, JSON
  shape, resource budgets, and error behavior are unchanged.
- `ocrdoc` selectors are renamed `imagepreflight.*` to `picture.*`
  (same identifiers otherwise); only the import lines and the qualifier
  changed (plus gofmt import ordering). No alias in either direction: the
  leaf no longer shadows the standard library, so both the leaf and its
  tests import stdlib `image` plain.
- The moved README's evidence commands are corrected since those lines
  were already in this diff: both name the new leaf path.
- No wire, API, fact, reducer, query, graph, storage, queue, lease,
  concurrency, or runtime behavior changes.

## Verification

All Go commands ran from `go/` with `env -u GOROOT`, a worktree-local
`GOCACHE`, and `GOTMPDIR=/tmp/e6695image`, serially:

- `go build ./internal/collector/preflight/picture/ ./internal/collector/ocrdoc/`:
  exit 0.
- `go vet ./internal/collector/preflight/picture/ ./internal/collector/ocrdoc/`:
  exit 0.
- `go test -count=1 ./internal/collector/preflight/picture/ ./internal/collector/ocrdoc/`:
  exit 0 (`preflight/picture` ok, `ocrdoc` ok).
- `gofmt -l` on both touched trees: clean.
- `scripts/verify-package-docs.sh`, `scripts/verify-telemetry-coverage.sh`,
  `scripts/verify-moved-file-refs.sh` (3 vacated Go paths, no dangling refs),
  `scripts/verify-filename-stutter.sh`,
  `scripts/verify-doc-citations.sh`: all pass on the committed HEAD.

Deliberately not run (orchestrator promotion gates): `make pre-pr`,
`make pre-pr-full`. Promotion ran `eshu-code-review` (self-review, READY,
P0/P1/P2-blocking 0), `review-attest` capture/verify, `make pre-push`
(all local gates passed), and the strict docs build
(`mkdocs build --strict --clean`).

## No-Regression Evidence (#6695):

- Baseline: branch base `origin/main` at `b7618bb2c`, measured in a
  throwaway worktree (`/tmp/img-base`, removed after the run) on the
  old path `go/internal/collector/imagepreflight`.
- After: branch `feat/6695-image-leaf` at the refactor commit,
  new path `go/internal/collector/preflight/picture`.
- Backend/version: no live backend exercised. Unit tests only against the
  in-repo classifier; no NornicDB, Postgres, or Docker in these packages.
  Toolchain `go1.27.1 darwin/arm64` both sides, same machine, serial runs.
  Wall times are same-machine relative readings, not reference targets.
- Input shape: `go test -count=1` on the old path (base) and the new path
  (branch); `go test -list '.*'` for test discovery.
- Baseline measurement: `imagepreflight` ok 0.386s (6 tests), `ocrdoc`
  ok 0.352s.
- After measurement: `preflight/picture` ok 0.201s (same 6 tests,
  confirmed by name via `-list`); importer `ocrdoc` ok 0.313s.
- Terminal counts: 2/2 test-bearing touched packages green, zero failures.
- Query/concurrency proof: the moved `preflight.go` pairs at 99%
  similarity (package-clause delta only); the added-line
  scan of the Go diff finds no Cypher/SQL keywords, no telemetry
  identifiers, and no new goroutine or error-path lines — logic is
  byte-identical to base.
- Telemetry/log/status evidence: added-line identifier scan
  (metric/counter/histogram/gauge/tracer/otel/prometheus/pprof) returns
  empty; `go vet` clean on the touched tree.
- Why the change is safe: rename-only nest with one mechanical importer
  repoint (compiler-checked by the collector-tree build); the relocated
  tests pass in the baseline time band.

## No-Observability-Change (#6695):

- The `telemetry-coverage.md` row for image preflight names the new
  `preflight/picture/*.go` path with the same `No-Observability-Change`
  reason: git-source collector metrics cover image preflight throughput.
- `scripts/verify-telemetry-coverage.sh` agrees: no new untracked stages.
