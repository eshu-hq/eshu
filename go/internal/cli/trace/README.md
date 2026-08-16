# CLI Trace Family

## Purpose

Builds and renders `eshu trace service`, the command that answers "how does this
service get from source code to something running". It requests the canonical
service-story envelope, decodes it, and writes the two views an operator sees:
the trace summary, and the candidate list when a service name matched more than
one service.

The command itself — the cobra tree, the flags, the API client, the process exit
code — lives in `go/cmd/eshu`. This package holds everything that does not need
those.

## Ownership boundary

Owned here:

- the request path and query string (`FetchServiceStory`)
- the envelope types the response decodes into (`ServiceEnvelope`,
  `ServiceError`)
- the two renderings (`RenderServiceSummary`, `RenderServiceError`) and the
  helpers behind them
- the two envelope readings the command branches on
  (`ServiceFreshnessState`, `ServiceStatus`)

Not owned here, and deliberately so:

- cobra flag reading and the `--json` decision — `go/cmd/eshu/trace.go`
- the concrete API client, and the envelope `Accept` header it sends
- the mapping from an envelope error code to a process exit code. That table is
  `traceExitCode` in `go/cmd/eshu`, shared with the `map`, `component_api`, and
  `change impact` families, and it stays there because those families still call
  it.
- classifying a transport failure into an error code
  (`traceErrorCodeFromTransport`, same reason)

`go/cmd/eshu` is `package main`, so nothing can import it. That is why the seam
runs in this direction: the command imports this package, never the reverse.

## Exported surface

`EnvelopeFetcher`, `ServiceEnvelope`, `ServiceError`, `ServiceQuery`,
`FetchServiceStory`, `RenderServiceSummary`, `RenderServiceError`,
`ServiceFreshnessState`, `ServiceStatus`. See [`doc.go`](doc.go) for the
godoc-rendered contract.

`EnvelopeFetcher` is a one-method interface declared here because it is consumed
here; `*eshu.APIClient` satisfies it without knowing this package exists.

## Dependencies

Standard library only: `encoding/json` (via the caller), `fmt`, `io`,
`net/url`, `strings`. No cobra, and no other internal package.

The no-cobra rule is machine-checkable:

```bash
cd go && go list -deps ./internal/cli/trace | rg spf13
```

Empty output is the passing result.

## Telemetry

None. This package emits no metrics, spans, or logs — it renders to the
`io.Writer` its caller hands it and returns errors upward. Command-level
telemetry stays in `go/cmd/eshu`.

## Gotchas / invariants

**Renderer output is a contract.** Operators read these lines and scripts parse
them. Tests here pin the summary and the ambiguous-candidate list byte for byte,
and `go/cmd/eshu/trace_test.go` pins them again through the command. Reordering
or rewording a line is a behavior change.

**Transport errors travel unwrapped.** `FetchServiceStory` returns the client's
error as-is. `go/cmd/eshu` prints that text verbatim and matches substrings of it
("connection refused", "request failed") to classify the failure, so wrapping it
would change both the operator's message and the process exit code. The
`//nolint:wrapcheck` on that function is load-bearing, not noise.

**The envelope readers are one copy among siblings.** `mapValue`, `sliceValue`,
`stringValue`, and `intValue` in `value.go` match the same-named sets in
`internal/cli/change`, `internal/cli/freshness`, and `internal/cli/component`;
`stringsValue` matches component's. All were forked from `go/cmd/eshu`
originals (`traceMap` and friends) that nothing could import across the
`package main` boundary and that are gone now -- #6139 and #6059 removed their
last callers there. Change one copy and you must change the rest:
`TestEnvelopeReaderParity` in `go/cmd/eshu` compares the copies at the source
level per reader and names the one that drifted, and the entitymap twin tests
pin that family's set against this one.

This copy carries no bool reader. The family's `boolValue` is absent here
because nothing in this package reads a boolean out of an envelope, and the
`unused` linter rejects a reader kept only for symmetry. The parity test
registers that absence and fails if `boolValue` reappears here unregistered.
It matches on the names the other copies use, so a bool reader added under a
name none of them uses passes it — register any bool reader you add rather
than relying on the test to catch it.

**An empty section is omitted, not printed empty.** A code-to-runtime trace with
no segments renders nothing at all, header included. A heading with no rows would
tell an operator a path was found when none was.

**Two envelope shapes are load-bearing.** `downstream_consumers` arrives as a
list in some responses and an object in others, and `intValue` accepts `float64`
because `encoding/json` decodes every JSON number into one. Both are covered in
`render_test.go` and `value_test.go`; dropping either arm silently reports zero.

## Related docs

- [`go/cmd/eshu/AGENTS.md`](../../../cmd/eshu/AGENTS.md) for the command wrapper's rules
- [HTTP API Reference](../../../../docs/public/reference/http-api.md) for the
  service-story endpoint this family calls
