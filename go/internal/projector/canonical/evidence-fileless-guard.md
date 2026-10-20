# Fileless-guard no-regression evidence (eshu-hq/eshu#7671)

`rejectSilentFilelessWipe` runs once per `BuildMaterialization`, before the
directory-chain build. Common builds exit through two cheap checks
(non-first/non-delta test, then `len(mat.Files) > 0`); the fact scan, map
build, and sorts run only when the materialization carries zero File rows,
the shape that previously wiped prior File nodes and reported success.

## Measurements

No-Regression Evidence: the guard adds about 8 ns to a normal build and
about 0.35 ms only on the fail-loud path that replaces a silent graph wipe.

| Case | Input shape | Result (3 x 1M iters) |
|---|---|---|
| Fast path (normal build, `Files` non-empty) | nil facts, nil quarantined | 8.12, 8.21, 8.14 ns/op |
| Degenerate scan (1000 live file facts, zero File rows) | 1000 synthetic live file envelopes | 353.8, 402.4, 354.4 us/op |

- Baseline: pre-guard `BuildMaterialization` ran no filelessness check.
- After: `go test ./internal/projector/canonical/ -bench 'ScratchGuard'
  -benchtime=1000000x -count=3 -run '^$'`, exit 0.
- Backend/version: `go version go1.26.9 linux/arm64` on the dev host.
- Terminal counts: `go test ./internal/projector/canonical/...` ok;
  runtime and decode suites ok; `go vet` and `gofmt` clean.
- Why safe: the fast path is one call plus two comparisons with zero
  allocations, dwarfed by the surrounding build work. The scan path runs
  only when the build is about to fail loudly instead of wiping the graph,
  so its cost never lands on a successful projection.

## Correlation truth

Positive case (dropped live facts fail loud), negative cases (first-gen,
delta, all-tombstone builds still succeed), and the quarantine accounting
are pinned by `builder_fileless_guard_test.go` (3 tests, RED first). No
fixture truth changed, so no compose re-verification applies.

## Observability

No-Observability-Change: the guard emits no log, metric, or span. Failure
surfaces as a `BuildMaterialization` error through the existing
projection-failure path, whose callers treat a non-nil error as
authoritative. No new operator signal was added.

## Empty-scope marker (eshu-hq/eshu#8668)

A scope that is genuinely fileless declares it on its repository fact with
the `empty_scope` payload key (same in-band declaration pattern as
`delta_generation` / `reconciliation_generation` in `delta.go`; only an
explicit true declares, through `decode.PayloadBoolPtr`). The builder
carries the declaration on
`CanonicalMaterialization.DeclaredEmptyScope`, pinned by
`builder_empty_scope_test.go` (marked succeeds with the flag set, unmarked
zero-facts succeeds with the flag clear, marker plus dropped live facts
still fails loud).

The declaration excuses only builds that carry zero file facts: it is the
escape hatch for the #8657 writer-side conditional retract, which must fail
a file-less wipe over prior File nodes unless the scope declared
filelessness. It never excuses dropped live facts.

Producer handoff: no collector emits the key yet. The declaration belongs to
the collector that walked the scope (`RepositoryFactEnvelope` in
`go/internal/collector/git/content/envelopes.go`, sent from `streamFacts` in
`go/internal/collector/repo/git/fact_builder.go`): declare only when the
snapshot carries zero file facts and zero deleted paths outside a delta,
and never when discovery reported errors. That wiring rides with the #8657
lane, which needs the hatch live for its writer check.

No-Regression Evidence: `go test ./internal/projector/canonical/...`,
`./internal/projector/decode/...`, `./internal/projector/runtime/...`
`-count=1` green; `go vet ./internal/projector/...` clean. The derivation is
one repository-fact filter plus one map lookup beside the existing
delta/reconciliation derivation, so no new hot-path cost class.

No-Observability-Change: same as above; the carried flag is builder-local
until the #8657 writer check reads it.
