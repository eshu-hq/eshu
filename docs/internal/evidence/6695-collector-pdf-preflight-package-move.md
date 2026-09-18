# #6695 collector pdf preflight package move

## Scope

Base: `origin/main` at `5368607dd` (post-#6764).

This slice moves the PDF safety-classifier package from the historical
flat path `go/internal/collector/pdfpreflight` to
`go/internal/collector/preflight/pdf`. The package name changes to
`pdf`; `Options`, `Result`, `Warning`, `Preflight`, and the format and
warning constants remain exact. This is the second `preflight/` leaf (the
`archive` leaf landed as #6762); the diagram, image, media, ooxml, and
manifest siblings stay flat for later slices.

Filenames are unchanged (`preflight.go` does not repeat its own `pdf`
directory, per naming.md rule 2 and the landed `preflight/archive`
precedent which kept `preflight.go`).

The `go/internal/collector/preflight` documentation-only namespace trio
(`doc.go`, `README.md`, `AGENTS.md`) landed with the archive leaf, so no
new parent trio is added here. The parent `README.md` now names both the
`archive` and `pdf` children.

## Inventory and dependency edges

At the base, the leaf contained exactly five files: `AGENTS.md`,
`README.md`, `doc.go`, `preflight.go`, and `preflight_test.go` (two
non-test Go files). The destination contains those same five file names.

The destination leaf's production imports remain standard-library only
(`bytes`, `context`, `fmt`, `io`, `path/filepath`, `sort`, `strings`).
It imports neither the collector root nor a sibling collector. No dirgate
ledger row changes: nothing pinned is touched, and no collector-root file
repeats the new `preflight/pdf` path, so no naming finding is introduced.

Zero Go importers outside itself: `rg -l "collector/pdfpreflight" go
--glob '*.go'` at the base returns no hits (the bare name appears only in
the leaf's own three Go files), and the same `rg` census on the rebased
HEAD is empty — the old path is fully vacated. The remaining `pdfpreflight`
mentions are the five own files plus one row each in the telemetry-coverage
table, the architecture review, the ecosystem-migration table, and the
1737 visual-media design doc — all repointed in this slice. No
`.golangci.yml`, surface-inventory, replay, or command wiring changes are
needed. No forwarding package or duplicate classifier remains.

## TDD evidence

Rename-only move; no behavior touch. The repoint proof is test discovery
at the new path:

```text
go test -list '.*' ./internal/collector/preflight/pdf/
TestPreflightAcceptsNormalPDFMetadata
TestPreflightClassifiesUnsupportedMalformedAndLimits
TestPreflightClassifiesUnsafePDFMarkers
TestPreflightClassifiesScannedLikePDF
TestPreflightClassifiesCanceledContextAsTimeout
TestPreflightResultJSONOmitsSourceAndPDFContent
```

All six run green with `-count=1`, as does the one routing test in the
owning `gitrepo` package (`-run
'PDFDocumentationFormatsRemainDefaultOff'`, confirmed selected via
`-list`).

## Preserved contracts

- `Preflight`, `Options` normalization, warning classes, counts, JSON
  shape, resource budgets, and error behavior are unchanged.
- No importer selectors exist to rename (zero importers); only the three
  package clauses and the package doc comment changed in Go sources.
- The moved README's second evidence command named
  `go test ./internal/collector -run ...`, which selects zero tests: the
  routing test lives in the `gitrepo` package as
  `TestPDFDocumentationFormatsRemainDefaultOff`. That package path is
  corrected in the moved README since the line was already in this diff.
- No wire, API, fact, reducer, query, graph, storage, queue, lease,
  concurrency, or runtime behavior changes.

## Verification

All Go commands ran from `go/` with `env -u GOROOT`, `GOCACHE` and
`GOTMPDIR` under `/tmp/e6695pdf`, serially:

- `go build ./internal/collector/preflight/...`: exit 0.
- `go vet ./internal/collector/preflight/...`: exit 0.
- `go test -count=1 ./internal/collector/preflight/...`: exit 0
  (`preflight` parent has no test files; `archive` ok; `pdf` ok).
- `go test -count=1 ./internal/collector/gitrepo/ -run 'PDFDocumentationFormatsRemainDefaultOff'`:
  exit 0 (1 test selected, confirmed via `-list`).
- `gofmt -l` on the touched tree: clean.
- `scripts/verify-package-docs.sh`, `scripts/verify-telemetry-coverage.sh`,
  `scripts/verify-moved-file-refs.sh`, `scripts/verify-dirgate.sh --files`
  (touched files), `scripts/verify-performance-evidence.sh`: exit 0.
- Pre-commit hooks on the refactor commit (including `golangci-lint` on
  changed packages, filename-stutter, package-docs, and no-AI-attribution
  checks): all passed.

Deliberately not run (orchestrator promotion gates): `make pre-push`,
`make pre-pr`, `make pre-pr-full`, `review-attest`, `eshu-code-review`.

## No-Regression Evidence (#6695):

- Baseline: `origin/main` at `441f1c468`; the old-path package directory
  is byte-identical between the clean main checkout (`3ff493935`) and
  `441f1c468` (empty `git diff` stat), so the timing below (measured on
  the clean main checkout with its own `GOCACHE`) is a valid baseline.
- After: branch `feat/6695-preflight-pdf` at the refactor commit.
- Backend/version: no live backend exercised. Unit tests only against the
  in-repo classifier; no NornicDB, Postgres, or Docker in these packages.
  Toolchain `go1.27.1 darwin/arm64` both sides, same machine, serial runs.
  Wall times are same-machine relative readings, not reference targets.
- Input shape: `go test -count=1` on the old path (base) and the new path
  (branch); `go test -list '.*'` for test discovery.
- Baseline measurement: `pdfpreflight` ok 0.454s (6 tests).
- After measurement: `preflight/pdf` ok 0.348s (same 6 tests, confirmed
  by name via `-list`); untouched sibling `preflight/archive` ok 0.483s;
  `gitrepo` routing test ok 0.707s (1 test).
- Terminal counts: 2/2 test-bearing touched packages green, zero failures.
- Query/concurrency proof: the moved `preflight.go` pairs at 99%
  similarity (package-clause-only delta); the added-line scan of the Go
  diff (4 added lines, all package clauses) finds no Cypher/SQL keywords,
  no telemetry identifiers, and no new goroutine or error-path lines —
  logic is byte-identical to base.
- Telemetry/log/status evidence: added-line identifier scan
  (metric/counter/histogram/gauge/tracer/otel/prometheus/pprof) returns
  empty; `go vet` clean on the touched tree.
- Why the change is safe: rename-only nest with zero importers (nothing
  to repoint, compiler-checked by the preflight-tree build); the
  relocated tests pass in the baseline time band.

## No-Observability-Change (#6695):

No operator-facing signal changes. No metrics, spans, structured-log
fields, status endpoints, or pprof knobs are added or altered — the
added-line telemetry scan above is empty, and warning-class strings
(`unsupported_format`, `malformed_pdf`, ...) are unchanged values,
not new signals.
