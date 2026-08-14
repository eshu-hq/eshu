# Report Bundles

## Purpose

`report` owns the logic behind `eshu report capture` and `eshu report
validate`. Capture issues the query a reporter says returned a wrong answer,
composes the answer into a `wrong_answer_report.v1` bundle through
`internal/reportbundle`, and hands back the bundle plus the exact bytes to
write. Validate decodes a bundle somebody sent back, runs it through the same
package's share-safe gate, and writes a one-line verdict.

The bundles exist to be attached to a public GitHub issue, so what reaches the
output matters as much as what the query returned.

## Ownership boundary

This package owns capture and validation *logic*: screening the reporter's
target, decoding `--params`, building the request URL, reading the observed
truncation flag and query profile off the response envelope, and turning the
result into bytes. It does not own process wiring — reading cobra flags,
resolving the process's streams, or mapping an error to an exit code. Those
stay in `go/cmd/eshu/report_cmd.go`, the cobra `RunE` wrapper, because
`go/cmd/eshu` is `package main` and nothing can import it.

Composing, redacting, digesting and validating the bundle itself belongs to
`internal/reportbundle`. This package resolves the answer and supplies the
inputs; it adds no redaction rule of its own beyond the two target rules below.

## Exported surface

- `CaptureBundle` — screens the target, issues the query through an
  `EnvelopeClient`, and returns a `CaptureResult` (the `reportbundle.Bundle`
  and its indented, newline-terminated JSON)
- `CaptureOptions` — the reporter's raw flag values: `Endpoint`, `Tool`,
  `Method`, `ParamsJSON`, `Note`, `IncludePayloads`
- `CaptureResult` — the finished bundle and the bytes to write for it
- `EnvelopeClient` — the two envelope calls this package needs, declared here
  because the concrete client lives in `package main`
- `TargetCredentialError` — a target carrying a credential, typed so the CLI
  can pick the usage exit code without matching message text
- `WriteBundle` — writes a captured bundle owner-only (`0600`)
- `ReadBundleInput` — reads a bundle from a path, or from the supplied
  `io.Reader` when no path is given
- `ValidateBundle` — decodes, validates, and writes the verdict line to an
  `io.Writer`
- `IncludePayloadsWarning` — the stderr text for `--include-payloads`

See `doc.go` for the godoc contract.

## Dependencies

- `internal/reportbundle` — `Capture`, `Validate`, `SplitTargetQuery`, and the
  `Bundle` shape. `SplitTargetQuery` is shared deliberately: the request URL and
  the recorded target come from one function, so they cannot drift.
- `internal/query` — `ResponseEnvelope`, `QueryProfile`, and the truth envelope
  stored verbatim in the bundle
- Consumed by `go/cmd/eshu`: the `report capture` and `report validate`
  subcommands (`report_cmd.go`)

`spf13/cobra` is absent from this package's dependency graph
(`go list -deps ./internal/cli/report | rg spf13` returns nothing), which is
the machine-checkable form of the no-process-wiring rule.

## Telemetry

None. Capture and validate run inline with the CLI invocation against one HTTP
call or one local file; there is no background pipeline stage to instrument.
Errors are returned for the wrapper to print.

## Gotchas / invariants

- **The request keeps the credential; the bundle does not.** Reproducing the
  reporter's exact query is the feature, and the answer under investigation is
  the one their credential returns. Only the recorded artifact is redacted.
- **A target carrying URL userinfo is refused, not stripped.** Every rule in
  `internal/reportbundle` matches an object key name, and `SplitTargetQuery`
  turns a target's query string back into keys so those rules can reach it.
  Userinfo sits before the `?`, so no key name exists to match — a password
  once reached `query.target` of a bundle stamped `"profile": "public"`,
  `"rules": []` and `"status": "passed"`. Refusing keeps the bundle honest
  about what was asked; only the reporter can supply a credential-free target.
- **`net/url` decides what an authority is.** An `@` inside a path segment
  (`/api/v0/owners/dev@example.com/services`) is not userinfo and still works.
  Do not replace this with a character rule.
- **What the target rules do not cover:** a credential under a benign parameter
  name (`internal/reportbundle`'s own documented limit), a full URL pasted
  inside a path segment (not an authority, so `net/url` reports no userinfo),
  and a server that echoes the request URL inside a 4xx/5xx body — that arrives
  as `go/cmd/eshu`'s `apiHTTPError`, which `internal/cli` cannot read.
- **`requestErrorWithoutURL` re-checks the path `CaptureBundle` already
  screened.** That is deliberate: the guard should not depend on which caller
  ran first.
- **An explicit `--params` entry replaces a same-named endpoint parameter**, in
  the request and in the record, matching how `reportbundle.Capture` resolves
  the same collision.
- **`--tool` changes the recorded surface and target only.** `--endpoint` still
  resolves the answer; this slice records an MCP capture, it does not invoke
  MCP.

## Related docs

- `go/internal/reportbundle/README.md` — the bundle schema, its redaction rules
  and its share-safe gate
- `go/cmd/eshu/report_cmd.go` — the cobra wrapper showing how the halves fit
