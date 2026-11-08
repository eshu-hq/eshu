# #10415 Full-Pass Fallback: No-Regression and Observability Evidence

Bounded full-pass fallback for scoped holder passes with out-of-stamp stale
rows. A scoped-path-only probe (`hasOutOfStampActiveSupplyChainImpactRows`)
checks for active finding rows outside the stamp; when found (or on probe
error, fail-closed), the pass runs full instead of scoped.

## No-Regression Evidence

- Baseline: scoped passes load evidence inside the stamp only; full passes
  load everything. No probe exists, so no probe cost.
- After: full passes are byte-identical in behavior and cost (they never
  probe). Scoped passes add exactly one `LIMIT 1` read shaped to the
  existing `fact_records_supply_chain_impact_active_scan_idx` partial-index
  predicate (EXPLAIN ANALYZE 0.012ms on disposable PG18); empty stamps skip
  the read entirely.
- Backend/version: disposable PostgreSQL 18.6 (pg-10415), schema migrated to
  the lane head.
- Input shape: synthetic fallback proof rows (two CVEs across two scopes,
  one generation, strict fencing tokens mirroring production sequence).
- Terminal counts: `TestHasOutOfStampActiveRowsLive` PASS 4.69s;
  `TestScopedFallbackConvergesLive` PASS 0.14s; core package
  `go/internal/reducer/supplychain/core` GREEN; telemetry package GREEN.
- Why safe: the probe is read-only under the existing
  `withReadOnlyRepeatableRead` scope; it adds no lock, lease, claim, worker,
  queue, migration, or transaction scope. TOCTOU is benign (late drift reaped
  on the next reopen; early reap harmless). Fallback runs the long-existing
  full path, so the only new behavior is choosing it.

## Observability Evidence

- New mode label `scoped_fallback_out_of_stamp` on
  `eshu_dp_supply_chain_impact_pass_total`, enumerated alongside the three
  existing modes in `instruments.go`, `HOLDER_FANOUT.md`, and the
  reducer-domains coverage doc (telemetry-coverage verifier agrees).
- Probe-error path emits `h.Logger.WarnContext` with `scope_id`,
  `generation_id`, and `error`, guarded by `h.Logger != nil`, so an
  error-fallback spike reads distinct from drift reap at 3 AM.
- Sub-signal shape unchanged (`scoped_cve_count=0`,
  `scoped_evidence_envelopes=0` on fallback — comparable series).
