# AGENTS.md — go/internal/cli/playbooks guidance for LLM assistants

## Read first

1. `go/internal/cli/playbooks/README.md` — purpose, ownership boundary, and
   exported surface
2. `go/internal/cli/playbooks/doc.go` — the godoc contract
3. `go/cmd/eshu/playbooks.go` — the cobra wrapper that binds the `--input`
   flag, resolves the output stream and API client, and calls in here. It is
   the file that shows how the two halves fit together.
4. `docs/public/reference/query-playbooks.md` — the playbook catalog and the
   resolve contract on the server side

## Invariants this package enforces

- **No process wiring here.** The package declares no cobra flag, reads no
  environment variable, opens no file, and decides no exit status. `go/cmd/eshu`
  is `package main`, so nothing can import it; any symbol that reads a flag or
  builds the concrete client has to live in `go/cmd/eshu/playbooks.go` instead.
- **Envelope errors are output, not exit codes.** A response whose `error`
  member is set still prints and exits 0. Do not add exit-code mapping here
  without treating it as a behavior change with its own review — the freshness
  family maps codes, this family never has.
- **Transport errors pass through unwrapped.** The client's error text is what
  the operator sees; the inline `//nolint:wrapcheck` comments carry the reason.
  Wrapping would rewrite operator-visible text and break CLI parity.
- **A nil client fails loudly.** `RunList` and `RunResolve` return an error on
  a nil `EnvelopeClient`; `TestNilClientRecordsFailure` pins it. Do not turn
  that into a silent no-op — an unwired seam must never look healthy.
- **Print only after a successful fetch.** Nothing is written to the output
  writer when the transport fails, so a failing command never emits a partial
  JSON document.

## Common changes

- New field in the resolve response: extend the typed `resolved` member in
  `ResolveEnvelope` and cover it in the package test. The generic `Truth` and
  list `Data` maps pass server additions through without a code change.
- New input validation rule: change `ParseInputs` and its table test together.
  The error text is operator-visible; changing it changes the CLI contract.

## Verification

From `go/`:

```bash
go test ./internal/cli/playbooks ./cmd/eshu -count=1
```

`go/cmd/eshu/playbooks_test.go` closes the seam with the real `APIClient`
against an `httptest` server; keep it green when touching the request shape.
