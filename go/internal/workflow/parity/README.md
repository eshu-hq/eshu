# parity — workflow differential conformance harness

Owner: workflow extraction drive ([#8875](https://github.com/eshu-hq/eshu/issues/8875)).
Contract: `doc.go` (package godoc is authoritative).

## What this is

One scenario suite, parameterized by `Backend`, run against the in-tree
`ControlStore` on an isolated Postgres schema and against the satellite
claim plane on its own isolated database through the in-tree
`ClaimPlaneClient`. Identical seeded fixtures, deterministic IDs, one
fixed clock. The suite compares return values, error codes, and
read-plane-observable state.

## Dependencies

- In-tree leg: `storage/postgres` (`WorkflowControlStore`) over the
  database named by `PARITY_INTREE_DSN`, one isolated schema per run.
- Satellite leg: any workflow.v1 server at `PARITY_SATELLITE_URL`
  (local flip container or dark deployment) with direct DB access at
  `PARITY_SATELLITE_DSN` for truncation reset.
- No satellite module import: the HTTP leg speaks the protocol through
  the in-tree client only. No shared tables, ever.

## Operating the suite

```sh
go test ./internal/workflow/parity/ -count=1
```

With the legs configured, e.g. a local flip container on :18181 and two
isolated databases. Without the environment the suite skips. Never point
the satellite leg at an authoritative deployment, and never replay live
claims: synthetic run and scope IDs plus cleanup only.

## Telemetry

None: this is a test-only package with deterministic fixtures. Scenario
mismatches print both outcome records with the underlying error texts.
Throughput numbers log per leg for the dark-deployment no-regression baseline.

## Known divergences

- None on the claim path: an identical replay of an already-applied
  terminal mutation (complete, release, fail-retryable, fail-terminal)
  acks on both legs, a heartbeat after the complete stays rejected on
  both legs, and a replay after a reclaim stays rejected on both legs
  ([#9428](https://github.com/eshu-hq/eshu/issues/9428), strict
  `Outcome.Equal` since the replay-ack alignment).
- Commit pin: in-tree has none (`TestParityPinGap`); pin survival is
  proven on the satellite leg only.
