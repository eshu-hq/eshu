# API Error Boundary

## Purpose

`apierr` exists so a CLI command family can tell "the API said 404" from "the
API said 503" after its logic moves out of `go/cmd/eshu`.

The CLI's API client returns a concrete error type, `apiHTTPError`, that is
unexported and defined in `go/cmd/eshu/client.go`. That directory is
`package main`, so nothing in the repository can import it — not
`internal/cli`, not a test in another package, not a future command family.
Any code that classified a failure with `errors.As(err, &httpErr)` therefore
stops compiling the moment it moves into `internal/cli`.

This package is the seam. `go/cmd/eshu` keeps the concrete type and gives it an
`HTTPStatusCode() int` method; `internal/cli` packages depend on the interface
declared here.

## Ownership boundary

This package owns exactly one thing: reading an HTTP status code out of an
error chain. It owns no error vocabulary, no retry policy, no remediation text,
and no HTTP client. The mapping from a status to a CLI error code
(`not_found`, `backend_unavailable`, and the rest) belongs to the command
family that renders it, because different families answer differently — `map`
treats 409 as `ambiguous`, and nothing else does.

It has no dependency outside `errors` in the standard library, and that is a
constraint rather than an accident: every CLI package that classifies a
transport error will import this one, so anything added here is added to all of
them.

## Exported surface

- `HTTPStatusError` — the interface a transport error satisfies when it carries
  an HTTP status. One method, `HTTPStatusCode() int`.
- `StatusCode(err error) (int, bool)` — unwraps `err` looking for that
  interface. The bool is false when `err` is nil or when nothing in the chain
  carries a status, which callers must not confuse with a status of 0.

See `doc.go` for the godoc contract.

## Why `HTTPStatusCode` and not `StatusCode`

The concrete error in `go/cmd/eshu` already has a `StatusCode` field, and Go
does not allow a method and a field to share a name on one type. `HTTPStatusCode`
also reads unambiguously at a call site in a package that has no other status
codes in scope.

## Dependencies

- Standard library `errors` only.
- Implemented by `go/cmd/eshu`'s `apiHTTPError`; `client.go` carries a
  compile-time assertion so a rename there fails the build rather than silently
  dropping every consumer's classification.
- Consumed by `go/cmd/eshu`'s five error-classification sites today
  (`trace.go`, `map.go`, `investigation_cmd.go`, `hosted_setup_verify.go`,
  `diagnostics_classify.go`), and by the `internal/cli` packages those families
  extract into.

## Telemetry

None. Reading a field off an error carries no I/O and no pipeline stage to
instrument. The commands that call it own their own operator-facing output.

## Gotchas / invariants

- `StatusCode` matches on the interface, not on a concrete type. That is
  slightly wider than the `errors.As(err, &httpErr)` it replaced, and it is the
  point: any error that promises a status is classifiable. Only `apiHTTPError`
  implements it today.
- The bool result is load-bearing. `StatusCode(nil)` and
  `StatusCode(connectionRefused)` both return `(0, false)`; a caller that reads
  only the int treats an unreachable backend as HTTP 0 and picks the wrong
  branch.
- Do not add `Body` here. Nothing reads it through this interface, and adding
  it would freeze the concrete struct's shape for a reader that does not exist.

## Related docs

- `go/cmd/eshu/client.go` — the concrete `apiHTTPError` and the compile-time
  assertion binding it to `HTTPStatusError`
- `go/internal/cli/servicereport/README.md` — the same "package main cannot be
  imported" split, applied to a whole command family
