# Agent Notes: internal/cli/component

Command bodies for the `eshu component` family, extracted from `go/cmd/eshu`
under issue #6059. Read `README.md` and `doc.go` first, then the wrapper
side: `go/cmd/eshu/component.go` and `go/cmd/eshu/component_api.go`.

## Invariants

- No cobra, no `os.Getenv`, no exit-code mapping in this package. Flags,
  environment, streams, and the exit-code table stay in `go/cmd/eshu`; a Run
  function takes plain values and a writer and returns the error the command
  exits with. Logic added to the wrapper instead of here is logic nothing
  outside the binary can test.
- Rendered bytes and error text are the CLI's stable contract. Never add a
  wrapping prefix to a returned error, and never swap a JSON writer: the
  component payload and API envelope write with HTML escaping off, while the
  extraction-readiness and schema-versions surfaces keep the encoder default.
- The readers in `values.go` are pinned copies. `TestEnvelopeReaderParity`
  in `go/cmd/eshu` goes red if this set drifts from the originals or from
  the change/freshness sets. Edit every copy or none.
- The `eshu.component.cli.v1` schema version on `CLIOutput` only moves with a
  deliberate payload-shape change, with docs updated in the same PR.

## Common changes

- New subcommand: put the body here as `Run<Name>(w io.Writer, ...)`, the
  cobra command and flag resolution in `go/cmd/eshu/component.go`, and keep
  both sides' tests: wrapper wiring tests in `go/cmd/eshu`, body tests here.
- New API readback: extend `EnvelopeFetcher` consumers, keep the
  transport-failure classification (`traceErrorCodeFromTransport`) and exit
  mapping (`componentAPIEnvelopeError`) on the wrapper side.

## Failure modes

- A "small" wording change in a rendered string or error is a behavior
  change operators' scripts can branch on. Prove output parity against a
  binary built from the base commit before shipping.
- Removing a `//nolint:wrapcheck` here usually breaks the lint gate; adding
  a wrap to satisfy it instead breaks the operator-facing text. The nolint
  with its justification is the intended state.

## Do not change without an ADR or owner sign-off

- The exit-code semantics implied by returned errors (which stay mapped in
  `go/cmd/eshu`).
- The two JSON escaping conventions.
- The path-leak guarantees: `loadComponentIndex`'s fixed read-error message
  and the scaffold's refusal to write into an existing directory.
