# AGENTS.md — internal/reducer/workload/handle

Scoped instructions for this package. The root `AGENTS.md`
still applies.

- This is a reducer leaf package. It may import `go/internal/workloadid`,
  `go/internal/telemetry`, and the standard library. It must never import
  the parent `internal/reducer` package, `servicecatalog`, or
  `supplychain/core`; consumers depend on this package, never the reverse.
- `Resolve` is pure logic over injected candidates: no backend, no I/O, no
  clock. Graph reads stay at call sites behind `Lookup`, one
  `workload_name` index seek per handle batched per intent.
- A handle is never attached to more than one node. Keep the ambiguous
  outcome resolving to nothing, and keep `miss` distinct from `ambiguous`
  in both the outcome and the skip tally.
- Keep label values closed and bounded (`surface`, `outcome`). Never add a
  handle, repository, or scope id as a metric label; those belong on the
  structured log line only.
- The `SkipReasonHandleAmbiguous` string is a cross-file contract with the
  compatibility doc and every consumer tally: rename it only with all of
  them.
