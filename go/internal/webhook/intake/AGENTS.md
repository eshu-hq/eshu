# Webhook Intake Package Guidance

This subpackage stays in core at extraction: the parent `webhook/`
normalizers move with the listener, but the intake API is core's durable
storage front door and never moves with it.

Read these files first:

1. `doc.go`
2. `wire.go`
3. `api.go`
4. `api_test.go`

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
- Keep unknown JSON fields ignored (forward compatibility for newer
  listeners).

## Common Changes

- Add a trigger field in `wire.go` with an explicit JSON tag, a length check
  in the route's converter, and coverage in `api_test.go`.
- Add a route in `api.go` with its store interface in the `Stores` bundle
  and a parity case in the listener's `intake_parity_cases_test.go`.
- Change a receipt shape only with the listener submitter
  (`go/cmd/webhook-listener/intake_submit.go`) updated in lockstep.

## Do Not

- Do not accept provider-signed payloads on these routes. Provider trust
  ends at the listener.
- Do not widen the body cap without a measured intake-latency note.
- Do not return store internals in error bodies; providers only need the
  machine-readable code.
