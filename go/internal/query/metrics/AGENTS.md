# metrics — agent instructions

## Read first

`doc.go` for the route and middleware contract, `README.md` for the telemetry
and streaming invariants.

## Invariants

- The time-series route serves only the metrics in its allow-list; an
  unknown metric is a 400.
- No configured source means empty points with unavailable freshness, never
  an error.
- `RequestMiddleware` labels by matched route pattern and keeps forwarding
  `Flush` and `Hijack`.
- Change the capability row only in `capability.go`.
- This package must not import the root query package or another handler
  family.

## Common changes

Moving or renaming `request.go`: update the per-route latency row in
`docs/public/observability/telemetry-coverage.md`, which cites it by path.
