# CLI Playbooks

## Purpose

`playbooks` owns what the two `eshu playbooks` subcommands actually do:
`playbooks list` fetches the deterministic query-playbook catalog and
`playbooks resolve` turns one playbook plus operator inputs into bounded
calls. Both read the canonical Eshu response envelope and print it verbatim
as indented JSON. The package holds no state; given a client and options it
returns bytes and an error, which is what makes the request and rendering
contracts testable without building the binary.

## Ownership boundary

This package owns input parsing, the request paths and body shape, envelope
decoding, and JSON rendering. It does not own process wiring: reading cobra
flags, resolving the output stream, or building the concrete API client. Those
stay in `go/cmd/eshu/playbooks.go`, because `go/cmd/eshu` is `package main`
and nothing can import it.

The API side of these routes belongs to `internal/query`. This package is a
client of it and asserts nothing about how the envelope is produced.

## Exported surface

- `EnvelopeClient` — the two-method transport interface the caller supplies
  (`GetEnvelope`, `PostEnvelope`), declared here where it is consumed
- `ResolveOptions` — the resolve command's playbook ID and parsed inputs
- `ListEnvelope`, `ResolveEnvelope`, `EnvelopeError` — the canonical response
  shapes; list stays generic maps, resolve types the `resolved` member
- `ParseInputs` — raw `--input key=value` flag values to the request's input
  map, with trimming and loud rejection of malformed entries
- `RunList`, `RunResolve` — the whole command bodies: fetch, then print

See `doc.go` for the godoc-rendered contract.

## Dependencies

Standard library only. The transport arrives through `EnvelopeClient`; the
concrete client (`APIClient` in `go/cmd/eshu/client.go`) satisfies it and
sets the envelope Accept header.

## Telemetry

None. The commands print the envelope, including its `truth` block, and emit
no metrics, spans, or logs of their own.

## Gotchas / invariants

- An envelope-level `error` member exits 0: it is printed in-band as part of
  the JSON, unlike the freshness family, which maps error codes to exit codes.
- Transport errors are returned unwrapped on purpose — the client's message is
  the operator-visible text and `go/cmd/eshu` prints it verbatim. That is the
  wrapcheck carryover this package keeps from its `cmd/` origin.
- Both Run functions fail on a nil client rather than silently succeeding, so
  an unwired wrapper cannot pass as healthy. `TestNilClientRecordsFailure`
  pins this.
- Output is written only after a successful fetch; a failing command never
  emits a partial document.

## Related docs

- `docs/public/reference/query-playbooks.md` — what playbooks are, the
  catalog, and the resolve contract this package calls
