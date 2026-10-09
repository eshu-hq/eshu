# Activation Obligation Consumer

## Purpose

Settles the exact-generation activation obligations `ProjectorQueue.Ack`
writes (#7584): each worker claims one obligation, finalizes it, runs the
partition-scoped maintenance pass only when the generation's own phase is
missing, and finalizes again. Moved here from the flat `maintenance`
package under #7648.

## Ownership boundary

Owns the consumer loop, the settle state machine, the hold/inapplicable
taxonomy, and the census/housekeeping cycle. Does not own the obligation
table or its SQL (`storage/postgres/activation`), the maintenance pass
itself (`postgres.ActivationMaintainer`), or the `Service` side-runner
startup loop in the reducer root.

## Exported surface

- `Runner`, `Config` — the consumer and its bounds
- `Store` — storage port (`ClaimActivation`, `FinalizeActivation`,
  `RetireActivationInapplicable`, `CatchUpActivations`,
  `PruneActivations`, `ActivationStats`)
- `Maintainer` — maintenance port (`MaintainActivation`)
- `Obligation`, `FinalizeResult`, `CatchUpPage`, `Stats` — work shapes
- `Outcome*` — closed finalize outcomes
- `HoldError`, `Hold`, `HoldReasons`, `Hold*` — held-refusal taxonomy
- `ErrLeaseLost`, `ErrFinalizeLockTimeout`, `ErrInapplicable`,
  `ErrCatalogChanged` — terminal/expected failure markers

See `doc.go` for the full contract.

## Dependencies

- `internal/telemetry` — counters, gauges, settle span
- `pkg/log` — structured error logging

Never `internal/reducer`.

## Telemetry

- Metrics: `eshu_dp_activation_obligations{status}`,
  `eshu_dp_activation_obligation_oldest_open_age_seconds`,
  `eshu_dp_activation_obligation_claim_age_seconds`,
  `eshu_dp_activation_obligation_finalize_total{outcome}`,
  `eshu_dp_activation_obligation_woken_total`,
  `eshu_dp_activation_obligation_maintenance_duration_seconds{outcome}`,
  `eshu_dp_activation_obligation_catch_up_inserted_total`,
  `eshu_dp_activation_obligation_pruned_total`,
  `eshu_dp_activation_obligation_failures_total{reason}`
  (`finalize_lock_timeout` at Warn separates expected Finalize lock
  contention from `finalize` at Error)
- Spans: `reducer.activation_obligation_settle` (outcome, hold/failure
  reasons); a hold is a designed outcome, not a trace error
- Logs: `activation obligation finalized` (INFO),
  `activation obligation held: maintenance refused` (INFO),
  `activation obligation step failed` (`failure_class`
  `activation_obligation_<reason>`), catch-up notice

## Gotchas / invariants

- The maintenance port runs ONLY after a Finalize returned
  `phase_not_ready`. Never finalize again after a failed callback: the
  lease stays held and the obligation retries after it expires.
- On `ErrInapplicable` the runner retires the row and counts no
  maintenance failure. On a hold it keeps the lease, runs no fallback
  pass, and counts the hold reason.
- `ErrCatalogChanged` matches every `catalog_changed` hold via
  `errors.Is`; the hold-reason set is closed and `Hold` refuses any
  other reason so the label set stays closed.

## Related docs

- `go/internal/storage/postgres/activation/README.md`
- `docs/internal/evidence/7584-partition-scoped-maintenance.md`
- `docs/public/observability/telemetry-coverage.md`
