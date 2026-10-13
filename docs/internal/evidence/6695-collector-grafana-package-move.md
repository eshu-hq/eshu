# #6695 collector grafana package move

## Scope

Base: `origin/main` at `9784ab0ee1` (rebase target; worktree created at
`7aae3a6a6b`, three behind, rebased with no conflicts).

This slice moves the Grafana observer package from the historical flat path
`go/internal/collector/grafana` to
`go/internal/collector/observability/grafana`. The package name stays
`grafana`; all exported identifiers, qualified call sites
(`grafana.NewHTTPClient`, `grafana.TargetConfig`, `grafana.*` envelope
constructors), and filenames remain exact. This is the third
`observability/` leaf (after tempo #6764 and prometheus #6897); the loki
sibling stays flat for a later slice. No new parent trio: the
documentation-only `observability/{doc.go,README.md,AGENTS.md}` landed
with tempo, and this slice extends the parent `README.md` with the
`grafana` child sentences.

No filename stutter exists: no moved file repeats its `grafana`
directory name (`client.go`, `envelope.go`, `metrics.go`, `redaction.go`,
`source.go`, `types.go`), so no rename accompanies the move. No import
alias is needed either: the destination directory is still `grafana`, so
the two `collector-grafana` command importers keep byte-identical
qualified names with only the import path repointed; the replay fault
test repoints plus a gofmt import reorder into the `observability/` block.

## Inventory and dependency edges

At the base, the leaf contained exactly fifteen files: `AGENTS.md`,
`README.md`, `doc.go`, six non-test Go files (`client.go`,
`envelope.go`, `metrics.go`, `redaction.go`, `source.go`, `types.go`),
and six test files (`envelope_bench_test.go`, `envelope_test.go`,
`http_client_test.go`, `live_test.go`, `source_test.go`,
`truncation_test.go`). The destination contains those same fifteen file
names.

The destination leaf's production imports are unchanged: the collector
root (`CollectedGeneration`), `collector/sdk`, `facts`, `scope`,
`telemetry`, `workflow`, and the SDK factschema contracts. Like the
tempo leaf, this leaf retains a live dependency on the collector root,
which the parent `README.md` already records explicitly: a future
extraction of this subtree must address that coupling first. No dirgate
ledger row changes: nothing pinned is touched, and the destination leaf
(six non-test files) sits far below the 40-file cap.

The reverse importers are unchanged after substituting the package path:

- `go/cmd/collector-grafana/config.go` (production)
- `go/cmd/collector-grafana/service.go` (production)
- `go/internal/replay/inputtape/fault_collectors_test.go` (test-only)

The contract-encode adoption ratchet
(`go/internal/collector/contract_encode_adoption_test.go`) names the
leaf envelope by path relative to the collector root, so its grafana
entry moves from `grafana/envelope.go` to
`observability/grafana/envelope.go` in this slice (same follow-up shape
as the tempo and prometheus leaves). Expected encode calls are
unchanged.

No forwarding package or duplicate observer remains.

## TDD evidence

Rename-only move; no behavior touch. The repoint proof is test discovery
at the new path:

```text
go test -list '.*' ./internal/collector/observability/grafana/
```

All discovered tests run green with `-count=1`, as do the direct
consumers (`./cmd/collector-grafana`, `./internal/replay/inputtape`) and
the surface-inventory drift gate (`./internal/capabilitycatalog`,
`./cmd/capability-inventory`).

## Preserved contracts

- `NewHTTPClient`, `CollectObservedMetadata`, `NewClaimedSource`, the
  envelope constructors, fact kinds, redaction behavior, metric names, and
  error classes are unchanged.
- `collector:grafana` replay surface keys, metric names, environment
  variables, and the `collector-grafana` command binary name are unchanged;
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
  historical `go test` transcripts naming `./internal/collector/grafana`.
  Both are dated records of what ran at the time; repointing would
  falsify the record. The moved-file-reference guard only tracks vacated
  file paths, which neither transcript names, so both stay green.

## Verification

All Go commands ran from `go/` with `env -u GOROOT`, a worktree-local
`GOCACHE`, and `GOTMPDIR=/tmp/eshu-grafana-tmp`, serially. Gate scripts ran from
the repository root:

- `go build ./...`: exit 0.
- `go vet ./internal/collector/observability/... ./cmd/collector-grafana/... ./internal/replay/inputtape/... ./internal/capabilitycatalog/... ./cmd/capability-inventory/...`:
  exit 0.
- `go test -count=1 ./internal/collector/observability/... ./cmd/collector-grafana/... ./internal/replay/inputtape/... ./internal/capabilitycatalog/... ./cmd/capability-inventory/...`:
  exit 0.
- `go test -count=1 -run TestContractEncodeAdoptionRatchet ./internal/collector/`:
  exit 0 (fails before the ratchet repoint, passes after).
- `gofmt -l` on all touched Go files: clean.
- `scripts/verify-package-docs.sh`, `scripts/verify-telemetry-coverage.sh`,
  `scripts/verify-moved-file-refs.sh`, `scripts/verify-dirgate.sh`: exit 0.

Deliberately not run (orchestrator promotion gates): `make pre-push`,
`make pre-pr`, `make pre-pr-full`, `review-attest`, `eshu-code-review`.

## No-Regression Evidence (#6695):

- Baseline: `origin/main` at `7aae3a6a6b`; rename-only move, so the
  timing below (old paths on base, new paths on branch) compares the same
  tests relocated.
- After: branch `feat/6695-observability-grafana` post-rebase.
- Backend/version: no live backend exercised. Unit tests only (live
  Grafana tests gate behind credentials and skip); no NornicDB, Postgres,
  or Docker in these packages. Same machine, serial runs. Wall times are
  same-machine relative readings, not reference targets.
- Input shape: `go test -count=1` on the old paths (base) and the new
  paths (branch); `go test -list '.*'` for test discovery.
- Baseline measurement (clean main checkout at `9784ab0ee1`, isolated
  `GOCACHE`, serial): `collector/grafana` ok 0.052s,
  `cmd/collector-grafana` ok 0.014s.
- After measurement (branch at rebased HEAD, same machine, serial):
  `observability/grafana` ok 0.029s, `cmd/collector-grafana` ok 0.010s,
  `replay/inputtape` ok 0.035s — same band, no regression.
- Terminal counts: all test-bearing touched packages green, zero failures.
- Query/concurrency proof: all moved Go files pair at R100 or with
  package-clause-only or single-self-path-line deltas; added-line scan of
  the Go diff finds no Cypher/SQL keywords, no new goroutine, channel, or
  error-path lines — retry, rate-limit, and normalization logic is
  byte-identical to base.
- Telemetry/log/status evidence: added-line identifier scan
  (metric/counter/histogram/gauge/tracer/otel/prometheus/pprof) over
  `go/*.go` returns empty; the only scan hits tree-wide are four
  parent-`README.md` prose lines naming the unchanged Grafana row. The
  emitted metric names (`eshu_dp_grafana_*`) are unchanged values
  carried by the moved code, not new signals; `go vet` clean.
- Why the change is safe: pure nesting (`package grafana` unchanged in all
  files); compiler-checked importer repoints; relocated tests pass in
  the baseline time band.

## No-Observability-Change (#6695):

No operator-facing signal changes. No metrics, spans, structured-log
fields, status endpoints, or pprof knobs are added or altered — the
added-line telemetry scan over `go/*.go` is empty (only parent-README
prose names the unchanged row), the telemetry-coverage Grafana
row repoints only its path cell, and the `eshu_dp_grafana_*` series names
are unchanged.
