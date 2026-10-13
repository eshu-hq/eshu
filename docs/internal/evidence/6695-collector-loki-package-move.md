# #6695 collector loki package move

## Scope

Base: `origin/main` at `92178d63eac` (worktree created at the tip; no
rebase needed).

This slice moves the Loki observer package from the historical flat path
`go/internal/collector/loki` to
`go/internal/collector/observability/loki`. The package name stays
`loki`; all exported identifiers, qualified call sites
(`loki.NewHTTPClient`, `loki.TargetConfig`, `loki.*` envelope
constructors), and filenames remain exact. This is the fourth
`observability/` leaf (after tempo #6764 and prometheus #6897; grafana
#8049 runs in parallel on different rows). No new parent trio: the
documentation-only `observability/{doc.go,README.md,AGENTS.md}` landed
with tempo, and this slice extends the parent `README.md` with the
`loki` child sentences.

No filename stutter exists: no moved file repeats its `loki`
directory name (`envelope.go`, `http_client.go`, `http_normalize.go`,
`metrics.go`, `redaction.go`, `source.go`, `types.go`), so no rename
accompanies the move. No import alias is needed either: the destination
directory is still `loki`, so the two `collector-loki` command importers
and the two replay inputtape test importers keep byte-identical
qualified names with only the import path repointed.

## Inventory and dependency edges

At the base, the leaf contained exactly fifteen files: `AGENTS.md`,
`README.md`, `doc.go`, seven non-test Go files (`envelope.go`,
`http_client.go`, `http_normalize.go`, `metrics.go`, `redaction.go`,
`source.go`, `types.go`), and five test files (`envelope_test.go`,
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
(seven non-test files) sits far below the 40-file cap.

The reverse importers are unchanged after substituting the package path:

- `go/cmd/collector-loki/config.go` (production)
- `go/cmd/collector-loki/service.go` (production)
- `go/internal/replay/inputtape/acceptance_test.go` (test-only)
- `go/internal/replay/inputtape/fault_collectors_test.go` (test-only;
  stays sorted, so no gofmt reorder)

The contract-encode adoption ratchet
(`go/internal/collector/contract_encode_adoption_test.go`) names the
leaf envelope by path relative to the collector root, so its loki
entry moves from `loki/envelope.go` to
`observability/loki/envelope.go` in this slice (same follow-up shape
as the tempo, prometheus, and grafana leaves). Expected encode calls are
unchanged.

No forwarding package or duplicate observer remains.

## TDD evidence

Rename-only move; no behavior touch. RED first: with the ratchet still
naming `loki/envelope.go`, `TestContractEncodeAdoptionRatchet` fails
with `open loki/envelope.go: no such file or directory`. GREEN after
the repoint. The repoint proof is test discovery at the new path:

```text
go test -list '.*' ./internal/collector/observability/loki/
```

All discovered tests run green with `-count=1`, as do the direct
consumers (`./cmd/collector-loki`, `./internal/replay/inputtape`) and
the surface-inventory drift gate (`./internal/capabilitycatalog`,
`./cmd/capability-inventory`).

## Preserved contracts

- `NewHTTPClient`, `CollectObservedMetadata`, `NewClaimedSource`, the
  envelope constructors, fact kinds, redaction behavior, metric names, and
  error classes are unchanged.
- `collector:loki` replay surface keys, metric names, environment
  variables, and the `collector-loki` command binary name are unchanged;
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

- `docs/internal/evidence/4791-w1e-contract-evidence.md` keeps its
  historical `go test` transcript naming `./internal/collector/loki`.
  It is a dated record of what ran at the time; repointing would
  falsify the record. The moved-file-reference guard only tracks vacated
  file paths, which the transcript does not name, so it stays green.
- `docs/internal/design/4786-contract-integration-matrix.md` is a living
  design doc, not a transcript: its two cells naming the exact vacated
  file envelope under the old flat leaf path now resolve to nothing,
  so this slice repoints them at the nested path (same precedent as
  `0ac8734ef`, which repointed matrix refs for an earlier move).

## Verification

All Go commands ran from `go/` with `env -u GOROOT`, a `/tmp`
`GOCACHE`, and `GOTMPDIR=/tmp/eshu-loki-tmp`, serially. Gate scripts ran
from the repository root:

- `go build ./...`: exit 0.
- `go vet ./internal/collector/observability/... ./cmd/collector-loki/... ./internal/replay/inputtape/... ./internal/capabilitycatalog/... ./cmd/capability-inventory/...`:
  exit 0.
- `go test -count=1 ./internal/collector/observability/... ./cmd/collector-loki/... ./internal/replay/inputtape/... ./internal/capabilitycatalog/... ./cmd/capability-inventory/...`:
  exit 0.
- `go test -count=1 -run TestContractEncodeAdoptionRatchet ./internal/collector/`:
  exit 0 (fails before the ratchet repoint, passes after).
- `gofmt -l` on all touched Go files: clean.
- `scripts/verify-package-docs.sh`, `scripts/verify-telemetry-coverage.sh`,
  `scripts/verify-moved-file-refs.sh`, `scripts/verify-dirgate.sh`: exit 0.

Deliberately not run (orchestrator promotion gates): `make pre-push`,
`make pre-pr-full`, and CI.

## No-Regression Evidence (#6695):

- Baseline: `origin/main` at `c2e9140ee2`; rename-only move, so the
  timing below (old paths on base, new paths on branch) compares the same
  tests relocated.
- After: branch `feat/6695-observability-loki` post-rebase.
- Backend/version: no live backend exercised. Unit tests only (live
  Loki tests gate behind credentials and skip); no NornicDB, Postgres,
  or Docker in these packages. Same machine, serial runs. Wall times are
  same-machine relative readings, not reference targets.
- Input shape: `go test -count=1` on the old paths (base) and the new
  paths (branch); `go test -list '.*'` for test discovery.
- Baseline measurement (clean main checkout at `c2e9140ee2`, isolated
  `GOCACHE`, serial): `collector/loki` ok 0.013s,
  `cmd/collector-loki` ok 0.010s.
- After measurement (branch at rebased HEAD, same machine, serial):
  `observability/loki` ok 0.033s, `cmd/collector-loki` ok 0.023s,
  `replay/inputtape` ok 0.087s — same band, no regression.
- Terminal counts: all test-bearing touched packages green, zero failures.
- Query/concurrency proof: all moved Go files pair at R100 or with
  package-clause-only or single-self-path-line deltas; added-line scan of
  the Go diff finds no Cypher/SQL keywords, no new goroutine, channel, or
  error-path lines — the entire added surface is five import-path and
  ratchet-path repoints, and retry, rate-limit, and normalization logic is
  byte-identical to base.
- Telemetry/log/status evidence: added-line identifier scan
  (metric/counter/histogram/gauge/tracer/otel/prometheus/pprof) over
  `go/*.go` returns empty. The emitted metric names (`eshu_dp_loki_*`)
  are unchanged values carried by the moved code, not new signals;
  `go vet` clean.
- Why the change is safe: pure nesting (`package loki` unchanged in all
  files); compiler-checked importer repoints; relocated tests pass in
  the baseline time band.

## No-Observability-Change (#6695):

No operator-facing signal changes. No metrics, spans, structured-log
fields, status endpoints, or pprof knobs are added or altered — the
added-line telemetry scan over `go/*.go` is empty, the telemetry-coverage
Loki row repoints only its path cell, and the `eshu_dp_loki_*` series
names are unchanged.
