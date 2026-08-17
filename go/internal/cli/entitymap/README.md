# Entity Map

## Purpose

`entitymap` owns the logic behind `eshu map`: posting an entity-map request to
the API's `/api/v0/impact/entity-map` route, classifying what came back, and
rendering the canonical envelope as either a grouped text summary or JSON. The
package is named after the command with the Go keyword removed -- `map` cannot
be a package name -- and after the `entity_map` capability and route the
command reads.

## Ownership boundary

This package owns the request body, the outcome classification, and the two
output forms. It does not own process wiring: reading cobra flags, building the
API client, or turning a failure into a process exit code. Those stay in
`go/cmd/eshu/map.go`, the cobra `RunE` wrapper, because `go/cmd/eshu` is
`package main` and nothing can import it.

The exit code split is deliberate. `Resolve` classifies an outcome into a
`FailureKind`; `entityMapExitCode` in the wrapper turns a kind into the number
the process returns, so every exit code the binary can produce stays next to
the shared `traceExitCode` table.

The API itself owns what a map contains. This package sends the selectors the
operator gave, and reads a fixed set of members out of the response
(`status`, `from`, `resolution`, `sections`, `evidence`, `truth.freshness`);
everything else passes through to `--json` untouched.

## Exported surface

- `Options` -- one resolved request: `From`, `FromType`, `Repo`,
  `Environment`, `Relationship`, `Depth`, `Limit`, `JSON`
- `Envelope`, `EnvelopeError` -- the canonical response and its error member
- `EnvelopePoster` -- the one-method interface `Fetch` needs;
  `go/cmd/eshu`'s `*APIClient` satisfies it
- `Fetch` -- posts `Options` to the entity-map route and decodes the envelope
- `Resolve` -- turns a `Fetch` result into the envelope to render plus a
  `*Failure`, or `nil` on success
- `Failure`, `FailureKind` (`FailureEnvelope`, `FailureFreshness`,
  `FailureAmbiguous`, `FailureNoMatch`) -- the classified outcome
- `FreshnessState` -- reads `truth.freshness.state`
- `ErrorCodeFromTransport` -- classifies a transport error into a canonical
  envelope error code
- `Write`, `WriteJSON`, `RenderSummary`, `RenderError`, `RenderSection` --
  the output forms, all writing to a caller-supplied `io.Writer`

See `doc.go` for the godoc contract.

## Dependencies

- `internal/cli/apierr` -- `StatusCode`, to read the HTTP status off a
  transport error without naming `go/cmd/eshu`'s unexported error type
- Standard library only otherwise (`encoding/json`, `fmt`, `io`, `net/http`
  for its status constants, `strings`)
- Consumed by `go/cmd/eshu`: the `map` command (`map.go`)

No cobra import, and nothing that reads the process environment or opens a
file. `go list -deps ./internal/cli/entitymap | rg spf13` returns nothing.

## Telemetry

None. `eshu map` is one synchronous API read inside a CLI invocation; there is
no background stage to instrument. The API side owns the request's spans and
metrics.

## Gotchas / invariants

- **`ErrorCodeFromTransport` checks a 409 first, then message substrings,
  then the status.** A 409 means the selector was ambiguous, and only the
  entity map uses it that way. After that, `transportErrorCode` matches
  "connection refused" and "request failed" *before* looking at the status,
  so an error carrying both a status and a broken-connection message reports
  `backend_unavailable`. Reporting it as `invalid_argument` would send an
  operator to fix their selector instead of their backend.
  `TestErrorCodeFromTransportPrecedence` pins this with a status-400 error
  whose text says "connection refused".
- **`Resolve` checks freshness before resolution status.** An ambiguous map
  on a stale index reports the stale index: re-running the selector against
  truth that is known to be behind cannot resolve the ambiguity.
- **A transport error is replaced by a synthetic envelope** carrying only the
  classified error, so `--json` prints the same shape whether the API
  answered or the call never reached it.
- **`Write` drops a render error on the failing path** on purpose. The caller
  is about to report the classified failure, and replacing that with "write
  failed" would lose why the command failed.
- **An empty relationship section prints nothing, including its title**, so a
  map with two populated sections does not read as five sections that mostly
  failed.
- **The value readers in `values.go` are copies** forked from the `trace*`
  helpers in `go/cmd/eshu/trace.go`, not a shared dependency: that file is
  `package main`. Those originals are gone -- the component (#6139) and trace
  (#6059) extractions removed their last callers there -- so the copies are
  now held against the surviving sibling set in
  `go/internal/cli/trace/value.go`. When a shared `internal/cli` helper
  package for them lands, these are its first candidates for deletion. Until
  then the parity tests in `go/cmd/eshu/entitymap_parity_test.go` hold the
  copies to their twins: token-identical bodies for the readers, and a shared
  input table for the transport classifier, whose original **is** still
  declared in `go/cmd/eshu/trace.go`. `twin_source_test.go` in this package
  repeats the source checks from the focused
  `go test ./internal/cli/entitymap/` loop -- for the classifier it first
  normalises the `http.Status*` constants to the numbers the original
  spells out.

## Related docs

- `docs/public/reference/cli-reference.md` -- the `eshu map` command reference
- `go/internal/cli/apierr/README.md` -- how a CLI transport error's HTTP
  status crosses the `package main` boundary
