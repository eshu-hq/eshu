# Residual-pilot no-regression evidence (eshu-hq/eshu#7009)

The four residual pilots add two pure helpers on the gate's budget table in
`go/cmd/read-api-latency-gate/budget.go`: `Named` (one map lookup per route)
and `NamedRoutes` (one sort over the named-route list per render). This note
covers the push-gate evidence for that file.

## Measurements

No-Regression Evidence: the helpers add microseconds of harness-side work per
gate evaluation and change no production handler.

| Case | Input shape | Result |
|---|---|---|
| Gate package suite | `go test ./cmd/read-api-latency-gate/...` | ok, exit 0 |
| Race suite | `go test -race` on changed packages via `make pre-push` | PASS, exit 0 |
| Full-seed-scale live sweep | 4 pilots, methodology corpus | zero HardFailed, no validator rejection, p95 85-96ms vs 1000ms SLO |

- Baseline: the gate evaluated budgets without `Named`/`NamedRoutes`; the
  four residual rows had no gate coverage at all.
- After: helper cost is one hash lookup per route plus a single sort over
  dozens of named routes per render, dwarfed by the sweep's own read costs.
- Backend/version: `go version go1.26.9 linux/arm64` on the dev host.
- Why safe: harness-only code; no production query, handler, or store path
  is touched. A wrong pilot fails closed in CI (`sweep.go` converts a
  validator rejection into a 500-class failure), so a bad row reddens the
  gate instead of weakening it.

## Observability

No-Observability-Change: no log, metric, span, or status field was added.
Gate verdict output keeps its existing shape; the new rows render through
the same work-table path as their gated siblings.
