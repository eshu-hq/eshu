# #6695 collector tempo package move

## Scope

Base: `origin/main` at `441f1c468` (post-#6762).

This slice moves the Tempo observer package from the historical flat path
`go/internal/collector/tempo` to `go/internal/collector/observability/tempo`.
The package name stays `tempo`; all exported identifiers, qualified call
sites (`tempo.NewHTTPClient`, `tempo.TargetConfig`, `tempo.*` envelope
constructors), and filenames remain exact. This is the first
`observability/` leaf; the grafana, loki, and prometheusmimir siblings stay
flat for later slices.

No filename stutter exists: no moved file repeats its `tempo` directory
name (`client.go`, `envelope.go`, `http_normalize.go`, `metrics.go`,
`redaction.go`, `source.go`, `types.go`), so no rename accompanies the move.
No import alias is needed either: the destination directory is still
`tempo`, so the two `collector-tempo` command importers and the replay
fault test keep byte-identical qualified names with only the import path
repointed (plus gofmt import ordering in the fault test).

The new `go/internal/collector/observability` package is a
documentation-only namespace. It adds no runtime declaration, import, or
initialization. The `tempo` leaf owns metadata-only collection of Tempo
trace-signal metadata (source instances, tag names, bounded tag values,
coverage warnings). It is not an independently deployable service.
Provider access, fact emission, ACL handling, security review, and
telemetry stay in the owning collector slice.

## Inventory and dependency edges

At the base, the leaf contained exactly fifteen files: `AGENTS.md`,
`README.md`, `doc.go`, eight non-test Go files (`client.go`,
`envelope.go`, `http_normalize.go`, `metrics.go`, `redaction.go`,
`source.go`, `types.go`), and five test files (`envelope_test.go`,
`http_client_test.go`, `live_test.go`, `source_test.go`,
`test_helpers_test.go`). The destination contains those same fifteen file
names. The new namespace parent contains exactly three direct files:
`AGENTS.md`, `README.md`, and declaration-only `doc.go`.

The destination leaf's production imports are unchanged: the collector
root (`FactsFromSlice`, claim-source generation output), `collector/sdk`,
`facts`, `scope`, `telemetry`, `workflow`, and the SDK factschema
contracts. Unlike the `preflight/archive` leaf, this leaf retains a live
dependency on the collector root, which the new parent `README.md`
records explicitly: a future extraction of this subtree must address that
coupling first. No dirgate ledger row changes: nothing pinned is touched,
and the new parent (one non-test file) and destination leaf (eight
non-test files) sit far below the 40-file cap.

The reverse importers are unchanged after substituting the package path:

- `go/cmd/collector-tempo/config.go` (production)
- `go/cmd/collector-tempo/service.go` (production)
- `go/internal/replay/inputtape/fault_collectors_test.go` (test-only)

No forwarding package or duplicate observer remains.

## TDD evidence

Rename-only move; no behavior touch. The repoint proof is test discovery
at the new path:

```text
go test -list '.*' ./internal/collector/observability/tempo/
```

All discovered tests run green with `-count=1`, as do the direct
consumers (`./cmd/collector-tempo`, `./internal/replay/inputtape`) and
the surface-inventory drift gate (`./internal/capabilitycatalog`,
`./cmd/capability-inventory`).

## Preserved contracts

- `NewHTTPClient`, `CollectObservedMetadata`, `NewClaimedSource`, the
  envelope constructors, fact kinds, redaction behavior, metric names, and
  error classes are unchanged.
- `collector:tempo` replay surface keys, metric names, environment
  variables, and the `collector-tempo` command binary name are unchanged;
  only Go package paths moved.
- `specs/surface-inventory.v1.yaml` owner, proof-gate, and fixture paths
  are repointed, and
  `go/internal/capabilitycatalog/data/surface-inventory.generated.json`
  is regenerated with the sanctioned generator (`capability-inventory
  -mode generate`), never hand-edited. `catalog.generated.json` is
  byte-identical.
- No wire, API, fact, reducer, query, graph, storage, queue, lease,
  concurrency, or runtime behavior changes.

## Out of scope, left as found

- `docs/internal/design/4786-contract-integration-matrix.md` and
  `docs/internal/evidence/4791-w1e-contract-evidence.md` keep their
  historical `go test` transcripts naming `./internal/collector/tempo`.
  Both are dated records of what ran at the time (shared
  `GOCACHE=/tmp/eshu-codex-gocache-4791` transcript); repointing would
  falsify the record. The moved-file-reference guard only tracks vacated
  file paths, which neither transcript names, so both stay green.

## Verification

All Go commands ran from `go/` with `env -u GOROOT`, a worktree-local
`GOCACHE`, and `GOTMPDIR=/tmp/e6695-tmp`, serially. Gate scripts ran from
the repository root:

- `go build ./...`: exit 0.
- `go vet ./internal/collector/observability/... ./cmd/collector-tempo/... ./internal/replay/inputtape/... ./internal/capabilitycatalog/... ./cmd/capability-inventory/...`:
  exit 0.
- `go test -count=1 ./internal/collector/observability/... ./cmd/collector-tempo/... ./internal/replay/inputtape/... ./internal/capabilitycatalog/... ./cmd/capability-inventory/...`:
  exit 0.
- `gofmt -l` on all touched Go files: clean.
- `scripts/verify-package-docs.sh`, `scripts/verify-telemetry-coverage.sh`,
  `scripts/verify-moved-file-refs.sh`, `scripts/verify-dirgate.sh`: exit 0.

Deliberately not run (orchestrator promotion gates): `make pre-push`,
`make pre-pr`, `make pre-pr-full`, `review-attest`, `eshu-code-review`.

## No-Regression Evidence (#6695):

- Baseline: `origin/main` at `441f1c468`; the old-path tempo tree is
  byte-identical between `f89b05014` and `441f1c468`, so the timing below
  (measured on a clean main checkout) is a valid baseline for both.
- After: branch `feat/6695-observability-tempo` post-rebase.
- Backend/version: no live backend exercised. Unit tests only (live Tempo
  tests gate behind credentials and skip); no NornicDB, Postgres, or Docker
  in these packages. Toolchain `go1.27.1 darwin/arm64` both sides, same
  machine, serial runs. Wall times are same-machine relative readings, not
  reference targets.
- Input shape: `go test -count=1` on the old paths (base) and the new
  paths (branch); `go test -list '.*'` for test discovery.
- Baseline measurement: `collector/tempo` ok 0.647s,
  `cmd/collector-tempo` ok 0.552s (10 tests, confirmed by name via `-list`).
- After measurement: `observability/tempo` ok 0.540s,
  `cmd/collector-tempo` ok 0.516s, `replay/inputtape` ok 0.737s (same 10
  tempo tests by name, plus untouched inputtape coverage).
- Terminal counts: all test-bearing touched packages green, zero failures.
- Query/concurrency proof: all 13 moved Go files pair at R100/R097 with
  package-clause-only or single-self-path-line deltas; added-line scan of
  the Go diff finds no Cypher/SQL keywords, no new goroutine, channel, or
  error-path lines — retry, rate-limit, and normalization logic is
  byte-identical to base.
- Telemetry/log/status evidence: added-line identifier scan
  (metric/counter/histogram/gauge/tracer/otel/prometheus/pprof) returns
  empty; the emitted metric names (`eshu_dp_tempo_*`) are unchanged values
  carried by the moved code, not new signals; `go vet` clean.
- Why the change is safe: pure nesting (`package tempo` unchanged in all
  13 files); compiler-checked importer repoints; relocated tests pass in
  the baseline time band.

## No-Observability-Change (#6695):

No operator-facing signal changes. No metrics, spans, structured-log
fields, status endpoints, or pprof knobs are added or altered — the
added-line telemetry scan above is empty, the telemetry-coverage Tempo
row repoints only its path cell, and the `eshu_dp_tempo_*` series names
are unchanged.
