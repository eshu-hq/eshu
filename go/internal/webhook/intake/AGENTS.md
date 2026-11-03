# Webhook Intake Package Guidance

This subpackage stays in core at extraction: the parent `webhook/`
normalizers move with the listener, but the intake API is core's durable
storage front door and never moves with it.

Read these files first:

1. `doc.go`
2. `wire.go`
3. `api.go`
4. `api_test.go`
5. `api_cancel_test.go` (disconnect and cancellation proofs)

## Invariants

- Keep provider signature verification out of this package. The listener
  verifies; the intake API authenticates the listener, validates the wire
  version, bounds the body, validates the normalized trigger, and persists.
- Do not add fact emission, graph writes, or queue handoff here. Stores own
  durability; the coordinator owns handoff.
- Keep the bearer token out of logs, errors, and metric labels. Log route,
  provider family, status, and machine-readable reason only.
- Keep `api_version` negotiation fail-closed: absent or `v1` passes, anything
  else is `unsupported_api_version` before storage.
- Keep every request-path metric label a fixed set (`metrics.go`): never a
  delivery id, repository name, or unbounded wire string. A new label value
  needs a bounded mapper and a test in `metrics_test.go`.
- Keep a failed durable write labelled `status="failed"` on the decision
  series: the freshness alert selects on it.
- Keep spans bounded (`spans.go`): `webhook.handle` and `webhook.store` carry only
  the closed attribute set `provider`, `event_kind`, `decision`, `outcome`, `reason`,
  `status`; never a trigger id, delivery id, repository, account, or scope id, and
  never `RecordError` the raw store error. The store call must receive the context
  `beginStore` returns so the stores' spans nest under `webhook.store`
  (`spans_test.go` pins it). Each handler rebinds the request with `obs.bind(r)`
  so intake log lines carry the handle span's trace_id and span_id
  (`spans_logs_test.go`).
- Keep the AWS and GCP freshness counters to `intake_stored`,
  `intake_coalesced`, and `intake_failed` with GCP `auth_path` set to
  `telemetry.AuthPathNotApplicable`. Do not emit `intake_rejected` or
  `intake_ignored` here (`freshness_events_test.go` pins it).
- Keep unknown JSON fields ignored (forward compatibility for newer
  satellite senders).

## Common Changes

- Add a trigger field in `wire.go` with an explicit JSON tag, a length check
  in the route's converter, and coverage in `api_test.go`.
- Add a route in `api.go` with its store interface in the `Stores` bundle
  and a parity case in `api_test.go` (the listener's `intake_parity_cases_test.go`
  was removed with it in #8876).
- Change a receipt shape only with the satellite submitter
  (`eshu-hq/eshu-webhook`, which carries a wire-version test) updated in
  lockstep.

## Do Not

- Do not accept provider-signed payloads on these routes. Provider trust
  ends at the satellite.
- Do not widen the body cap without a measured intake-latency note.
- Do not return store internals in error bodies; providers only need the
  machine-readable code.
