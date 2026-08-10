# AGENTS.md — mock-prometheus-mimir guidance

## Read first

1. `README.md` for the test fixture's scope.
2. `server.go` for the exact accepted request and response.
3. `go/internal/query/metrics_prometheus.go` for the production client contract.
4. `go/cmd/api/metrics_source.go` for environment-driven source selection.

## Invariants

- Keep the server credential-free and public-safe. Do not add real tokens,
  tenants, endpoints, or recorded provider responses.
- Accept only the closed queue-depth query, one-hour window, and five-minute
  step used by the golden proof.
- Return two in-range samples with values `2` then `0`. An empty matrix would
  let the deployed capability pass without positive history.
- Keep `/health` separate from `/api/v1/query_range` and restrict both to GET.
- Do not add Postgres, graph, collector, or product telemetry dependencies.

## Common changes

If the production queue-depth PromQL mapping changes, update the fixture's exact
query and its hostile test in the same diff. If the proof moves to another
logical metric, keep one closed expression rather than accepting arbitrary
PromQL.

## Failure modes

- HTTP 400 means the request query, range, step, or parameter set drifted.
- HTTP 401 means `X-Scope-OrgID` did not carry the synthetic fixture tenant.
- HTTP 404 or 405 means the caller used the wrong path or method.

## Anti-patterns

- Do not turn this into a general Prometheus emulator.
- Do not make malformed requests return a successful empty matrix.
- Do not use fixed timestamps outside the requested range.
