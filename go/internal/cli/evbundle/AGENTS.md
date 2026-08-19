# AGENTS.md — go/internal/cli/evbundle guidance for LLM assistants

## Read first

1. `go/internal/cli/evbundle/README.md` — purpose, ownership boundary, and
   the "what the artifact screens, and what it does not" enumeration. Read
   that section before touching anything that reaches a bundle field.
2. `go/internal/cli/evbundle/doc.go` — the godoc contract.
3. `go/cmd/eshu/evidence.go` — the cobra `RunE` wrapper that
   resolves flags, builds the API client, supplies the clock, and maps errors
   to exit codes. This is where the two halves fit together.
4. `go/internal/evidencebundle/AGENTS.md` — the composer this package feeds.
   Bundle shape, validation rules, and the canary patterns live there, not
   here.

## Invariants this package enforces

- **No process wiring.** No cobra flags, no reads of Eshu config or a
  credential from the process environment, no `os.Stdin`/`os.Stdout`, no
  `time.Now`, no `os.Exit`. `go/cmd/eshu` is `package main`, so nothing can
  import it — a symbol that reads a flag or maps to an exit code belongs in
  `evidence.go` instead.

  `ReadBundleInput` and `WriteBundle` call `os.ReadFile` / `os.WriteFile` on a
  path parameter the caller supplies. That is not process wiring; it is the
  same "act on an explicit parameter" shape as
  `internal/cli/mcpsetup`'s `WriteMCPServerConfig`. Do not push those into the
  wrapper.
- **Validate before stamp, and return nothing on a rejection.**
  `StampValidation` writes `validation.status: passed`; applying it without a
  nil `Validate` certifies a check that never ran. A rejected export returns
  `nil` bytes plus the error, so no caller can write a partial rendering.
- **Compose no strings that reach a bundle field.** `LiveSnapshotFromStatus`
  copies decoded values through verbatim. The moment something here builds a
  bundle value with `fmt.Sprintf`, ask what can be interpolated into it — an
  endpoint, a target, a token, a DSN — because a validator that screens by
  shape only catches the shapes it knows.
- **Any failing status route fails the whole live export.** Never fall back to
  a zero value for a route that did not answer.

## Common changes and how to scope them

- **Carry a new status field into the bundle** → add the field to the decode
  type in status.go, map it in `LiveSnapshotFromStatus`, and check whether
  `internal/query/evidence_bundle_live.go` already carries it. That route
  composes the same bundle from a typed `status.Report`, and
  `go/cmd/eshu/evidence_bundle_api_parity_test.go` fails when the two
  readings diverge. Extend that test's fixture too — a field only one side
  populates is exactly what it exists to catch.
- **Change the bundle's shape, bounds, or validation** → that is
  `internal/evidencebundle`, not here.
- **Add a status route** → add the constant, the decode type, and a
  `FetchLiveSnapshot` step. Failing the export on a bad response is the
  contract, not an option.
- **Change an operator-visible message** → the strings are pinned by
  `cmd/eshu`'s command tests and by the exported-function tests here. Update
  both; do not add a wrapping layer to satisfy `wrapcheck`, which is why the
  two `//nolint:wrapcheck` directives exist.

## Failure modes and how to debug

- Symptom: `export --live` fails with `validate live evidence bundle: private
  endpoint is not allowed…` → a status route returned free text carrying a
  locating address. The refusal is correct. Fix the source of the text or
  broaden the screen in `evidencebundle`; do not drop the field here.
- Symptom: a count in the bundle is zero when the stack is busy → suspect a
  json tag in status.go before suspecting the composer.
  `TestFetchLiveSnapshotReadsTheThreeStatusRoutes` asserts every non-zero
  value in its fixture bodies for this reason.
- Symptom: the CLI and the API route disagree → the parity test in
  `go/cmd/eshu` names the field. One side is carrying something the other
  drops; check `LiveSnapshotFromStatus` against
  `internal/query/evidence_bundle_live.go`.

## Anti-patterns specific to this package

- **Reaching into `go/cmd/eshu`.** It cannot be imported. Add a parameter.
- **Sharing `internal/cli/scan`'s `PipelineStatus`.** The decode types here
  are deliberately separate; see README.md's Ownership boundary.
- **A canary keyed on field names.** Every screen that matters here runs over
  the marshalled document. A test that plants its sentinel under a key called
  `password` proves nothing about the leak shape that has actually bitten this
  repo.
- **Widening the exported surface to make a test easier.** The command tests
  in `go/cmd/eshu` already drive the whole path end to end.

## What NOT to change without an ADR

- Allowing `--scope` with `--live`. All three status routes are stack-global;
  a repository-scoped label would attribute every other repository's queue and
  collector state to that one repository.
- Moving `evidencebundle.Validate` out of the export path, or stamping a
  bundle before it. Both would make `validation.status: passed` a claim
  nothing checked.
