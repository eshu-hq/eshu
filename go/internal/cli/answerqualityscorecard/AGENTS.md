# AGENTS.md — go/internal/cli/answerqualityscorecard guidance for LLM assistants

## Read first

1. `go/internal/cli/answerqualityscorecard/README.md` — purpose, ownership
   boundary, exported surface, and the redaction section
2. `go/internal/cli/answerqualityscorecard/doc.go` — the godoc contract
3. `go/cmd/eshu/answer_quality_scorecard_cmd.go` — the cobra `RunE` wrapper
   that resolves process state and calls in here. This is the file that shows
   how the two halves fit together.
4. `go/internal/answerquality/AGENTS.md` — the scoring package whose `Verdict`
   this one formats. Scoring rules do not belong here.

## Invariants this package enforces

- **No process wiring.** No cobra flags, no Eshu config, no process
  environment reads, no `os.Exit`, no touching the process's real `os.Stdin` or
  `os.Stdout` — the wrapper passes `cmd.InOrStdin()` / `cmd.OutOrStdout()` in
  as `io.Reader` / `io.Writer` parameters. `go/cmd/eshu` is `package main`, so
  nothing can import it; any symbol that reads a flag or maps to an exit code
  has to live in `answer_quality_scorecard_cmd.go` instead.

  `ReadEvidence` calling `os.ReadFile` on its `path` parameter is NOT an
  exception to this. Acting on an explicit argument is mechanical input
  handling, the same shape as `internal/cli/servicereport`'s `ReadInput`. Do
  not push it into the wrapper.
- **No scoring here.** `answerquality.ParseEvidence` and `answerquality.Score`
  are called by the wrapper, not by this package. A scoring rule, threshold, or
  criterion that appears in this directory is in the wrong place.
- **`RenderVerdict` is text-mode only.** The `--json` path marshals
  `answerquality.Verdict` directly and never enters this package. Do not route
  JSON through `RenderVerdict` or vice versa.
- **No redaction here, and do not add any.** See below.

## Redaction rules for this directory

This package emits an artifact (a verdict) built from an artifact it was given
(captured evidence), so every string it prints is a potential copy path.

- `RenderVerdict` prints the run id and each criterion's detail **verbatim**.
  The run id is a raw copy of the captured artifact's `run_id`.
- The only thing making that safe is the `publish_safety` criterion in
  `internal/answerquality` (which calls `internal/answerguardrail`). A value
  that screen accepts is printed here unchanged.
- **Do not add a redaction pass to this package.** Two screens of differing
  strength on one path make it ambiguous which is authoritative, and the weaker
  one silently becomes the contract. If the screen is too narrow, widen the
  screen — in its own change, with its own regression test, because it also
  runs on the live Ask publish path.

If you add ANY new value to the rendering, extend
`TestRenderVerdictCopiesOnlyTheDocumentedFieldsIntoText` in the same change and
state whether the new value is screened upstream. That test plants a sentinel
inside a value (never under a key) and varies the character immediately before
it, because a screen or renderer that behaves differently after a colon than
after a space is a defect this repository has shipped before.

## Common changes and how to scope them

- **Change the text layout** → edit `RenderVerdict` and its private helpers in
  `scorecard.go`. The JSON shape is untouched by this package.
- **Accept a new input source** → follow the `ReadEvidence` shape: take the
  source as a parameter, wrap the failure with `%w` naming what could not be
  read, and wire the new flag in the wrapper.
- **Add a criterion or change how one is scored** → wrong package. That is
  `internal/answerquality`.

## Failure modes and how to debug

- Symptom: the command reports a read failure naming a path the operator did
  not pass → check the wrapper's `--from` resolution first. `ReadEvidence`
  selects stdin only when the path is empty or all whitespace, and it never
  invents a path.
- Symptom: `--json` and the text output disagree → this package cannot be the
  cause. Both render the same `Verdict` the wrapper builds once, and
  `RenderVerdict` does not run during `--json` output.
- Symptom: a secret appears in the rendered verdict → this package is where you
  SEE it, not where it got through. Look at the `publish_safety` criterion in
  `internal/answerquality` and the fragment list in
  `internal/answerguardrail`'s `UnsafeString`.

## Anti-patterns specific to this package

- **Reaching into `go/cmd/eshu`.** It cannot be imported (`package main`). If
  logic here needs something only the wrapper has, add a parameter.
- **Importing `quoteIfEmpty` from anywhere.** The copy in `scorecard.go` is
  deliberate; the original in `go/cmd/eshu/first_run.go` is unreachable from an
  importable package and still has callers of its own. Do not delete either.
- **Adding a `fmt.Print*` call.** Everything this package writes goes through
  the `io.Writer` its caller supplies. Writing to the process's real stdout
  belongs in the wrapper.

## What NOT to change without an ADR

- Moving `answerquality.Score` or `ParseEvidence` into this package. The
  current split keeps the single `Score` call and the JSON-vs-text branch
  together in the wrapper; relocating either needs an explicit design decision,
  not an incidental refactor.
