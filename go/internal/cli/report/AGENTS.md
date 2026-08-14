# AGENTS.md — go/internal/cli/report guidance for LLM assistants

## Read first

1. `go/internal/cli/report/README.md` — purpose, ownership boundary, exported
   surface, and the list of what the target rules do and do not cover
2. `go/internal/cli/report/doc.go` — the godoc contract, including the
   redaction section
3. `go/cmd/eshu/report_cmd.go` — the cobra `RunE` wrapper that resolves flags,
   streams and exit codes and calls into this package
4. `go/internal/reportbundle/AGENTS.md` — the bundle schema, its redaction
   walk, and its share-safe gate. Every field-by-field rule lives there, not
   here.

## Invariants this package enforces

- **No process wiring.** No cobra flags, no Eshu config or credential read from
  the process environment, no `os.Exit`, no writing to the process's real
  stdout. The wrapper passes `cmd.InOrStdin()` / `cmd.OutOrStdout()` in as
  `io.Reader` / `io.Writer` parameters. `spf13/cobra` must stay out of the
  dependency graph — `go list -deps ./internal/cli/report | rg spf13` returning
  nothing is the check.

  `ReadBundleInput` and `WriteBundle` touch the filesystem through explicit
  path parameters. That is not process wiring, it is the same "act on an
  explicit parameter" shape `internal/cli/servicereport` uses. Do not push
  those into the wrapper.
- **A target carrying a credential is refused before the request goes out.**
  `checkTargetCredentials` runs on both `--endpoint` and `--tool` at the top of
  `CaptureBundle`. Checking only the one that becomes `query.target` leaves the
  other live: `--endpoint` reaches the bundle when `--tool` is absent, and
  reaches the failure message either way.
- **`net/url` decides what userinfo is.** Never replace `parsed.User != nil`
  with a character class or a regular expression. A boundary class that
  included a colon is how a private host reached a bundle stamped
  `redaction.rules: screened_private_endpoints` elsewhere in this repository.
- **An error never repeats the offending value.** These messages reach
  terminals, CI logs and pasted bug reports — the same egress the bundle beside
  them is redacted for. Name the flag or the field instead.
- **The request stays unredacted.** The reporter's credential goes on the wire
  because the wrong answer is the one that credential returned. Only the
  recorded artifact is cleaned.
- **`SplitTargetQuery` is shared with `reportbundle.Capture` on purpose.** The
  issued request and the recorded target come from one function. Do not add a
  second split here.

## Common changes and how to scope them

- **Add a capture input the reporter can supply** → add a field to
  `CaptureOptions`, decode or validate it inside `CaptureBundle`, and wire the
  flag in `go/cmd/eshu/report_cmd.go`. If it can hold an endpoint, a target, a
  token or a DSN, add it to `checkTargetCredentials`' call sites and to the
  boundary-varied canary in `redaction_test.go`.
- **Change what a bundle records or how a field is redacted** → that is
  `internal/reportbundle`, not here. This package supplies inputs.
- **Change the validation verdict lines** → `ValidateBundle`. The failed line
  is written before the error is returned, so a maintainer sees it whether or
  not they read the exit code.
- **Add a new exit code** → the mapping lives in the wrapper. Return a typed
  error from here (see `TargetCredentialError`) and have `runReportCapture`
  translate it; do not import an exit-code contract into this package.

## Failure modes and how to debug

- Symptom: capture refuses a target the reporter insists is fine → check
  whether `url.Parse` reports userinfo on it. An `@` in a path segment is
  accepted (there is a test); an `@` after `scheme://…:` is not.
- Symptom: a credential appears in the operator's terminal on a failed capture
  → check which error shape carried it. A `*url.Error` goes through
  `requestErrorWithoutURL`; an HTTP 4xx/5xx body does not, and cannot be
  reached from here until the `apiHTTPError` accessor from issue #6059's
  sibling branch (PR #6117) lands.
- Symptom: the request and the recorded bundle disagree about parameters →
  both derive from `reportbundle.SplitTargetQuery` plus the same collision
  rule. A disagreement means one side stopped using it.

## Anti-patterns specific to this package

- **Reaching into `go/cmd/eshu`.** It cannot be imported (`package main`). If
  new logic needs something only the wrapper has, add a parameter or a typed
  error.
- **Writing a redaction rule here.** Two target rules is the whole budget. A
  third rule that inspects a value belongs in `internal/reportbundle`, next to
  the walk that already does it and the tests that already cover it.
- **Testing redaction with a sentinel that only ever follows a space.** The
  character in front of a secret is what the last several defects hid behind.
  Vary it, and assert on the bytes written to disk as well as the returned
  bytes.
- **Stripping a credential instead of refusing one.** A bundle that quietly
  drops half of what the reporter asked misreports the query under
  investigation.

## What NOT to change without an ADR

- Moving bundle composition or redaction out of `internal/reportbundle` into
  this package. The current split keeps one redaction walk with one test suite;
  splitting it is a design decision, not a refactor.
- Making `CaptureBundle` redact the target rather than refuse it. The refusal
  is what keeps a share-safe bundle's `query.target` an honest record of the
  query under investigation.
