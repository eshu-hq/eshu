# AGENTS.md — go/internal/cli/apierr guidance for LLM assistants

## Read first

1. `go/internal/cli/apierr/README.md` — why the package exists and what it
   deliberately does not own
2. `go/internal/cli/apierr/doc.go` — the godoc contract
3. `go/cmd/eshu/client.go` — the concrete `apiHTTPError`, its `HTTPStatusCode`
   method, and the compile-time assertion that binds the two halves together
4. `go/cmd/eshu/trace.go` — `traceErrorCodeFromTransport`, the largest consumer
   and the function whose extraction this package unblocks

## Invariants this package enforces

- **One method on the interface.** `HTTPStatusError` promises a status code and
  nothing else. Five sites in `go/cmd/eshu` classify API errors and all five
  read the status; none reads the response body. Adding a second method obliges
  every future implementation to supply it, for a reader that does not exist
  yet.
- **No dependency outside `errors`.** Every CLI package that classifies a
  transport error imports this one, so a dependency added here lands in all of
  them. If new code needs an HTTP client, a config value, or a telemetry
  handle, it belongs in the calling package.
- **No status-to-code vocabulary here.** `not_found`, `backend_unavailable`,
  `ambiguous` and friends are per-family answers — `map` maps 409 to
  `ambiguous` and no other family does. Centralising them here would force one
  answer on families that legitimately differ.
- **`StatusCode` returns a bool, and callers must read it.** A missing status
  and a status of 0 are different facts. Do not simplify the signature to a
  bare `int`.

## Common changes and how to scope them

- **A new `internal/cli` package needs to classify API errors** → import this
  package and call `apierr.StatusCode(err)`. Do not redeclare a local
  `interface{ HTTPStatusCode() int }`; four divergent copies is the outcome
  this package was created to prevent.
- **A second error type starts carrying an HTTP status** → give it an
  `HTTPStatusCode() int` method and it is classifiable, no change needed here.
  Add a compile-time assertion at its definition site, the way `client.go`
  does.
- **A caller needs the response body too** → do not widen this interface. Read
  the body where the concrete type is still in scope (`go/cmd/eshu`), or open
  the package-boundary question deliberately, as issue #6059 did for the status
  code.

## Failure modes and how to debug

- Symptom: a CLI command reports `backend_unavailable` for a request that
  clearly returned a status → check that the caller reads the bool from
  `StatusCode` before branching on the int. A discarded bool turns "no status"
  into 0 and falls through to the caller's default branch.
- Symptom: every classification site suddenly returns the default code after a
  change to `go/cmd/eshu/client.go` → the concrete type stopped implementing
  the interface. The compile-time assertion in `client.go` catches a rename;
  it cannot catch the method being changed to a different receiver or return
  type in a way that still compiles, so re-run
  `go test ./cmd/eshu/ -run APIHTTPError`.

## Anti-patterns specific to this package

- **Exporting `apiHTTPError` instead of using this interface.** That freezes
  the struct's `Body` field into a public contract nobody reads. It was
  considered and rejected on issue #6059; reopening it needs the measurement
  redone, not an assumption.
- **Moving the API client in here.** `APIClient` has 20+ readers in
  `go/cmd/eshu` and cannot move before `resolveConfigValue` does. That is its
  own decision with its own sequencing, not a side effect of needing a status
  code.

## What NOT to change without an ADR

- Adding a second method to `HTTPStatusError`, or a second exported type to
  this package. The narrowness is the design; widening it is a boundary
  decision, not a refactor.
