# Fencing snapshot rehearsal: no-regression evidence (#9376)

The harness in this directory drives the fencing scenarios against the
deployed satellite. This note records why it cannot regress production:
it adds no production caller, and its live statements are bounded,
prefix-scoped, and run inside an operator-attended window.

## No-Regression Evidence:

- Baseline: new harness, no prior production load. No production code
  imports the parity harness (only non-test importer is a doc list), so
  the steady-state statement delta is zero.
- After: one live leg issues about 15 statements, every one scoped to
  the rehearsal instance or the `parity-` row prefix: 1 run insert, 1
  item insert, 2 claims, 1 reap, 4 pins, 1 complete, 1 snapshot SELECT,
  3 teardown DELETEs, 1 residue SELECT, and prefix-only dark resets.
- Backend/version: QA claim plane over HTTP, `go test -timeout 110m`
  time box, operator-attended window.
- Input shape and counts: exactly 1 seeded item per leg; no row-count
  or terminal-count change to any production table; no retry, backoff,
  or queue change. Measured live leg: 4.32 s wall, exit 0; plane
  interactions 0.12 s, remainder in-tree bootstrap migrations.
- Why safe: bounded rehearsal-only statements on a synthetic instance,
  prefix-scoped deletes that cannot reach other tenants, and a dark
  revert merged after the window. The intended behavior change is the
  fencing proof itself, shown by the four passing scenarios, not a
  timing shift.

## No-Observability-Change:

No new metrics, spans, or logs. The per-subtest Outcome record goes to
the test log only. Existing plane request instruments are untouched.
