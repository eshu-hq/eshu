# maintenance

## Purpose

Parent index of the reducer's periodic side-runner leaves. Each family
that used to live flat in this package now lives in its own leaf with
destuttered names (#7648); this directory holds the index only, no
runner logic.

## Ownership boundary

The leaves own their runners, policies, results, contracts, run loops,
and telemetry recording. This parent owns the leaf map and the shared
import rule below. No leaf owns intent claiming, graph projection, or
the `Service.Run` side-runner startup loop itself
(`Service.startSideRunners` stays in the reducer root, since it wires
every family's side-runner, not just this tree's).

## Leaves

| leaf | family | runner entrypoint |
|---|---|---|
| `accepted/` | repo-dependency activation-gate decorators | `GateOnActive`, `GatePrefetchOnActive` |
| `obligation/` | activation obligation consumer (#7584) | `Runner` |
| `evidence/` | collector-readiness evidence summary resweep (#3466) | `Maintainer` |
| `liveness/` | generation liveness recovery | `Runner` |
| `retention/` | generation retention pruning | `Runner` |
| `orphan/` | graph orphan sweep | `Runner` |
| `infra/` | infra read model reconcile (#6793) | `Runner` |
| `poison/` | poison dead-letter liveness (#4740) | `Runner` |
| `producer/` | producer activation consumer (#7635) | `Runner` |
| `testutil/` | shared metric-reading test helpers | `CounterValue`, `GaugeValue`, `HasAttrs` |

See each leaf's `doc.go` for its godoc-rendered contract.

## Dependencies

Every leaf may import `reducer/sharedintent`, `internal/telemetry`,
and `pkg/log`. No leaf may import `internal/reducer`, directly or
transitively.

Root-owned contracts are mirrored locally in the leaf that needs them:
`accepted.Lookup`/`accepted.Prefetch` are aliases over the same
unnamed signatures the root's `AcceptedGenerationLookup`/
`AcceptedGenerationPrefetch` define as named types, and
`orphan.PartitionLeaseManager` is a plain interface satisfied
structurally. `Prefetch`'s nested return type means the two packages'
prefetch spellings are not directly interchangeable the way the lookup
alone is; the one call site that crosses the boundary
(`cmd/reducer/main_helpers.go`) adapts with two thin wrapper closures
instead.

## Telemetry

Per-leaf signals live in the leaf READMEs. The tree registers no
parent-level instrument.

No-Regression Evidence: #6061 relocated this family's production logic
out of the flat `internal/reducer` root without changing it. Every hunk
in the moved production files was one of three kinds: a package clause;
(in `accepted_generation_active_gate.go` only) an
identifier-requalification from the root's
`SharedProjectionAcceptanceKey`/`SharedProjectionIntentRow` aliases to
`sharedintent.AcceptanceKey`/`sharedintent.Row` directly; or one of the
three locally-mirrored root declarations the move added.
Measured on that branch: `go build ./...` exits 0, `go vet ./...`
exits 0, `go test ./internal/reducer/... ./cmd/reducer
./internal/storage/postgres -count=1` passes.

No-Observability-Change: #6061 added no queue domain, worker, lease,
graph, or storage contract. The signals were the same before and after
the move.

No-Regression Evidence (#7648): this PR-2 move nests the 9 families
into leaf packages and strips the family compound prefix from every
exported identifier. Every hunk in the moved production files is a
package clause, an identifier rename from the published rename table,
or a doc-comment leading-name update; struct fields, method names,
and all string literals (metric, span, and log names included) are
byte-identical, verified by multiset comparison of methods and string
literals old vs new across all 28 moved files. Shared test helpers
hoist to `testutil` (`CounterValue`, `GaugeValue`, `HasAttrs`) with
identical bodies. Measured: `go build ./...` exits 0, `go vet ./...`
exits 0, `go test -count=1` over `maintenance/...`,
`storage/postgres/...`, `cmd/reducer/...`, `internal/reducer` passes
with the ok-set delta exactly the leaf reshuffle, and `go test -list`
discovers the same 81 tests before and after.

No-Observability-Change (#7648): this move adds no queue domain,
worker, lease, graph, or storage contract and renames no signal. All
metric, span, and log names are byte-identical before and after.

## Gotchas / invariants

- Test helpers are shared through `testutil`, never by exporting one
  leaf's (or the root's) test helper for another package to reach.
  Family-local helpers stay in the leaf's own test files.
- The `Service.startSideRunners` wiring proof for each runner stays in
  the reducer root (`TestServiceStarts*`), since `Service` and its
  unexported `startSideRunners` method are root-owned.
- `Prefetch`'s nested `Lookup` return type breaks the otherwise-free
  alias interop between the `accepted` leaf and the reducer root --
  do not assume every alias is a drop-in replacement for its root
  counterpart; the boundary adapter in `cmd/reducer/main_helpers.go`
  is load-bearing, not incidental.

## Related docs

- `go/internal/reducer/README.md`
- `go/internal/reducer/recovery-runners.md`
- `go/internal/reducer/sharedintent/README.md`
- `docs/public/observability/telemetry-coverage.md`
