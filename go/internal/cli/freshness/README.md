# CLI Freshness

## Purpose

`freshness` owns what the three `eshu freshness` subcommands actually do:
build a request path from the selectors an operator passed, read the canonical
Eshu response envelope, and write either that envelope as JSON or a human
summary. The three commands are `freshness generations` (scope generation
lifecycle history), `freshness changed-since` (what changed in a scope since a
prior generation or instant), and `freshness service-changed-since` (the same
diff for a service's evidence lineage).

The package runs no query and holds no state. Given a fetcher and a selector
set it returns bytes and an error, which is what makes the rendering and the
exit-code classification testable without building the binary.

## Ownership boundary

This package owns request-path construction, envelope decoding, summary
rendering, and error classification. It does not own process wiring: reading
cobra flags, resolving the output stream, or converting an error into the
CLI's exit-code type. Those stay in `go/cmd/eshu/freshness.go`,
`freshness_changed_since.go`, and `freshness_service_changed_since.go`, because
`go/cmd/eshu` is `package main` and nothing can import it.

The wrapper reads flags into an options struct, hands it and
`cmd.OutOrStdout()` to a `Run` function, and converts a returned `*Failure`
into `commandExitError`. That last step is the wrapper's alone — the exit-code
*number* is chosen here, the exit-code *type* is defined there.

The API side of these routes belongs to `internal/query`'s `FreshnessHandler`.
This package is a client of it and asserts nothing about how the envelope is
produced.

## Exported surface

Per command, three of everything:

- `GenerationsOptions` / `ChangedSinceOptions` / `ServiceChangedSinceOptions` —
  selector sets holding flag values exactly as cobra parsed them
- `GenerationsPath` / `ChangedSincePath` / `ServiceChangedSincePath` — request
  paths, with empty and whitespace-only selectors omitted and a non-positive
  limit left off so the server's default applies
- `FetchGenerations` / `FetchChangedSince` / `FetchServiceChangedSince` — one
  envelope GET each
- `RunGenerations` / `RunChangedSince` / `RunServiceChangedSince` — the whole
  command body: fetch, then write JSON or the summary
- `RenderGenerationsSummary` / `RenderChangedSinceSummary` /
  `RenderServiceChangedSinceSummary` — the human views
- `GenerationsRoute` / `ChangedSinceRoute` / `ServiceChangedSinceRoute` — the
  API route constants

Shared across all three:

- `EnvelopeFetcher` — the one-method transport interface the caller supplies
- `Envelope`, `EnvelopeError` — the canonical response shape
- `Failure` — the error a `Run` function returns when the command must exit
  non-zero, carrying the message and the numeric exit code
- `EnvelopeFailure` — envelope error to `*Failure`, with the message fallbacks
- `ExitCodeForErrorCode` — error code to exit code
- `ErrorCodeFromTransport` — transport failure to error code
- `RenderEnvelopeError`, `WriteJSON` — the failure line and the JSON output
- `ChangedSinceBaselineLabel` — what a scope diff is measured from

See `doc.go` for the godoc contract.

## Dependencies

- `internal/cli/apierr` — `StatusCode`, for reading the HTTP status off a
  transport error without naming `go/cmd/eshu`'s unexported error type
- Nothing else outside the standard library. `go list -deps` on this package
  returns no `spf13` entry and no `net/http`: the cobra dependency stops at the
  wrapper, and the HTTP client is the caller's.
- Consumed by `go/cmd/eshu`: the three freshness command files, plus
  `freshness_parity_test.go`, which drives this package against the real
  `internal/query.FreshnessHandler`

## Telemetry

None. These commands run inline with a single CLI invocation against the API;
there is no background stage to instrument. The API side emits the route
telemetry.

## Gotchas / invariants

- **A non-fresh index is reported, not an exit code.** `ExitCodeForErrorCode`
  maps error codes. A `truth.freshness.state` of `building` or `stale` prints
  in the summary and the command still exits 0. `eshu trace service`,
  `eshu change impact`, and `eshu map` all exit 4 on the same state, so do not
  "harmonize" the families without deciding to change these commands' exit
  codes on purpose. `TestRunGenerationsExitsZeroWhileTheIndexIsBuilding` and
  `TestExitCodeForErrorCodeRejectsBuildingSpelling` fail if someone does.
- **The exit-code table is one of two identical copies.** `traceExitCode` in
  `go/cmd/eshu/trace.go` is the original — `go/cmd/eshu` is package main and
  cannot be imported, so this family carries its own. `TestExitCodeTableParity`
  there holds both to the same answers, which matters more here than for the
  other shared helpers: an exit code is what an operator's script branches on,
  so a one-sided edit would surface as a pipeline taking the wrong branch, not
  as a failure anyone could trace back.
- **Message checks precede the status switch in `ErrorCodeFromTransport`.** An
  error carrying both an HTTP status and the text "connection refused"
  classifies as `backend_unavailable`. The `err != nil` guards on those two
  `strings.Contains` calls are required; the status branch needs none because
  `errors.As` reports false for a nil error. This function is one of three
  identical copies — the original `traceErrorCodeFromTransport` in
  `go/cmd/eshu/trace.go` and `change.ErrorCodeFromTransport` are the others —
  because `go/cmd/eshu` is package main and cannot be imported.
  `TestTransportErrorCodeParity` there holds all three to the same answers.
- **Nothing screens the rendered error message.** `RenderEnvelopeError` prints
  a transport error verbatim, and `net/http`'s text embeds the request URL —
  the `--service-url` endpoint and every selector in the query string. A secret
  an operator types into `--scope-id` appears in that line on stdout and again
  on stderr. `net/http` strips a URL's userinfo before this point, so a
  password in `--service-url` does not. `redaction_test.go` pins both halves.
- **The human summaries render an enumerated key set.** Anything else the
  server returns reaches `--json` and never the terminal summary. That is what
  `TestHumanSummariesRenderOnlyKnownKeys` asserts, with a `--json` presence
  check on the same input so a clean result cannot come from a sentinel that
  was never carried.
- **`--json` still fails.** The envelope is written and the `*Failure` is still
  returned, so `--json` never turns a non-zero exit into a zero one.
- **Whitespace-only selectors are dropped, not sent empty.** The path builders
  trim, so the wrapper can pass raw flag values straight through.
- **Renderers write through `writef`, not `fmt.Fprintf`.** The repo's wrapcheck
  linter exempts `go/cmd/*` but not `go/internal/cli/*`; funnelling the writes
  keeps the one `//nolint` in `write.go` instead of rewriting the
  operator-facing text of every write failure. See the comment there.

## Related docs

- `docs/public/reference/incremental-freshness-model.md` — what the three
  routes mean and what each command answers
- `go/internal/query/README.md` — the `FreshnessHandler` serving these routes
- `go/internal/cli/apierr/README.md` — the HTTP-status accessor
