# ask — agent instructions

## Read first

`doc.go` for the route contract, `README.md` for ownership and move evidence.

## Invariants

- The engine stays injected via `Asker`; a nil asker must keep answering 503,
  never nil-panic.
- Permission gates stay first: no engine call before the feature/data-class
  checks pass.
- Change the capability row only in `capability.go` — one `Support()`.
- `sse_metrics_middleware_test.go` pins the #3381 streaming regression
  against `metrics.RequestMiddleware`; keep it driving the real middleware.
- This package must not import the root query package.
