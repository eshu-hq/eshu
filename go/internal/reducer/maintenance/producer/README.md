# Producer Activation Consumer

## Purpose

Settles the producer-activation obligations `ProjectorQueue.Ack` writes
(#7635): each worker claims one obligation, reopens the dependent
consumer items, and completes it under the claim fence. Moved here from
the flat `maintenance` package under #7648.

## Ownership boundary

Owns the consumer loop, the settle state machine, and the census/
housekeeping cycle. Does not own the obligation table or its SQL
(`storage/postgres`), the dependency-index settle
(`postgres.ProducerActivationRunnerStore`), or the `Service`
side-runner startup loop in the reducer root.

## Exported surface

- `Runner`, `Config` — the consumer and its bounds
- `Store` — storage port (`ClaimProducerActivation`,
  `SettleProducerActivation`, `PruneProducerActivations`,
  `ProducerActivationStats`)
- `Activation`, `SettleResult`, `Stats` — work shapes
- `Outcome*` — closed settle outcomes
- `ErrLeaseLost`, `ErrSettleLockTimeout` — terminal/expected failure
  markers

See `doc.go` for the full contract.

## Dependencies

- `internal/telemetry` — counters, gauges, settle span
- `pkg/log` — structured error logging

Never `internal/reducer`.

## Telemetry

- Metrics: `eshu_dp_producer_activations{status}`,
  `eshu_dp_producer_activation_oldest_open_age_seconds`,
  `eshu_dp_producer_activation_claim_age_seconds`,
  `eshu_dp_producer_activation_settle_total{outcome}`,
  `eshu_dp_producer_activation_reopened_total{domain}`,
  `eshu_dp_producer_activation_pruned_total`,
  `eshu_dp_producer_activation_failures_total{reason}`
  (`settle_lock_timeout` at Warn separates expected settle lock
  contention from `settle` at Error)
- Spans: `reducer.producer_activation_settle` (outcome, failure
  reason); a lease loss is a designed outcome, not a trace error
- Logs: `producer activation settled` (INFO, scope, generation,
  outcome, reopened, claim token), `producer activation step failed`
  (`failure_class` `producer_activation_<reason>`)

## Gotchas / invariants

- There is no catch-up, by design: a generation whose consumers
  already replayed is indistinguishable from one that never did.
- On `ErrLeaseLost` the settle's transaction rolled back and the
  reopen did not survive; the next owner repeats it.
- Method names on the port are the storage contract;
  implementations in `storage/postgres` must match them exactly.

## Related docs

- `go/internal/storage/postgres/activation/README.md`
- `docs/public/observability/telemetry-coverage.md`
