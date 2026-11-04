# #9875 fleet acceptance gate: evidence

This change adds a test-only acceptance gate and registers its environment
variables. It adds no runtime code path, query, queue, lease or Cypher.

- No-Regression Evidence: the only non-test, non-doc source change is two
  entries in `go/internal/envregistry/entries.go` that name the gate's DSN and
  opt-in variables. The registry is read at startup to render docs and validate
  names; it is not on a request or worker path. Baseline and after are the same
  for every service binary: no behavior reads these entries at runtime.
  Backend: none touched. Input shape: not applicable. Row and queue counts: not
  applicable.
- No-Observability-Change: no metric, span, log key, status field or dashboard
  changes. The gate itself reports through its test output; an operator has no
  new signal to find because production behavior is unchanged.

The gate's own proof (seeded RED parent, GREEN on the clean tree) is described in
`docs/public/reference/local-testing/fleet-acceptance-gate.md`.
