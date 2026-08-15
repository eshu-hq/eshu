# Answer Quality Scorecard

## Purpose

`answerqualityscorecard` owns the input and output halves of `eshu
answer-quality-scorecard`: it reads a captured, redacted answer-quality
evidence artifact, renders the scored verdict as human-readable text, and
folds the failed criteria into the one-line summary the command's error
carries. It runs entirely offline against a local JSON file or stdin -- no
query, store, graph, or network path -- so the same input always produces the
same output.

Scoring is not here. `internal/answerquality` decodes the evidence
(`ParseEvidence`) and scores it (`Score`); this package handles the bytes going
in and the formatting coming out.

## Ownership boundary

This package owns evidence-reading and verdict-formatting *logic*. It does not
own process wiring: reading cobra flags, resolving the process's real stdin or
stdout, or mapping a result to an exit code. Those stay in
`go/cmd/eshu/answer_quality_scorecard_cmd.go`, the cobra `RunE` wrapper,
because `go/cmd/eshu` is `package main` and nothing can import it. The wrapper
resolves `--from` and `--json`, hands `cmd.InOrStdin()` and
`cmd.OutOrStdout()` in as plain `io.Reader` / `io.Writer` values, and returns
the error that becomes the non-zero exit.

`ReadEvidence` calls `os.ReadFile` on the path parameter its caller supplies.
That is acting on an explicit argument, not process wiring -- the same shape as
`internal/cli/servicereport`'s `ReadInput`. Do not "fix" it by pushing the file
read up into the wrapper.

## Exported surface

- `ReadEvidence(stdin, path)` -- returns the captured evidence bytes: the file
  at `path`, or everything on `stdin` when `path` is empty or all whitespace.
  A file-read failure names the path so the operator knows which artifact was
  unreadable. The bytes are returned undecoded; validation is
  `answerquality.ParseEvidence`'s job.
- `RenderVerdict(w, verdict)` -- writes the text view: a PASSED/FAILED header,
  the run and score lines, a rule, one line per scored criterion prefixed with
  a status marker, and then the title and labels of each follow-up issue.
- `FailureSummary(verdict)` -- joins every failed criterion as
  `name: detail` with `; ` separators, or returns `unknown failure` when the
  verdict failed and no criterion is marked failed.

Two unexported helpers back `RenderVerdict`: the status marker for a criterion
line, and the empty-run-id placeholder. See `doc.go` for the godoc contract.

## Dependencies

- `internal/answerquality` -- `Verdict`, `CriterionScore`, `CriterionStatus`,
  `FollowUpIssue` and the criterion-status constants. This package formats
  those values; it does not call `Score` or `ParseEvidence`, both of which run
  in the wrapper.
- Standard library only otherwise (`fmt`, `io`, `os`, `strings`).
- Consumed by `go/cmd/eshu`: the `answer-quality-scorecard` command.

`go list -deps` on this package resolves no `spf13` path, which is the
machine-checkable form of the no-cobra rule.

## Telemetry

None. The command runs inline with a single CLI invocation against a local
file or stdin; there is no background stage, queue, or worker to instrument.

## Redaction: what is and is not covered here

**Nothing is redacted in this package.** `RenderVerdict` prints the verdict's
run id and every criterion's detail text exactly as given, and both can carry
values copied straight out of the captured evidence artifact -- the run id is a
verbatim copy of the artifact's `run_id`.

The output's publish safety comes entirely from the `publish_safety` criterion
that `internal/answerquality` evaluates while building the verdict, which in
turn calls `internal/answerguardrail`. A value that screen accepts is printed
here unchanged.

Two consequences worth stating plainly:

- A gap in the upstream screen is a gap in this output. This package cannot
  and does not compensate for one.
- Do not add a second redaction pass here. Two screens of differing strength
  on one path make it ambiguous which is authoritative, and the weaker one
  becomes the de-facto contract. Fix the screen instead.

`TestRenderVerdictCopiesOnlyTheDocumentedFieldsIntoText` pins which parts of a
verdict reach the text rendering, so widening that set requires editing the
table rather than happening quietly.

## Gotchas / invariants

- `RenderVerdict` is text-mode only. The `--json` path in the wrapper marshals
  `answerquality.Verdict` directly and never calls into this package, so the
  JSON shape stays exactly what `answerquality` produces.
- The text renderer walks `Criteria` and `FollowUpIssues` only. `PromptScores`
  and a follow-up's `Detail` are carried in the JSON verdict but never
  printed.
- `quoteIfEmpty` is a copy of the helper in `go/cmd/eshu/first_run.go`, not a
  move. That file is `package main` with other callers of its own; the copy and
  the original are independent by construction.

## Related docs

- `go/internal/answerquality/README.md` -- the scoring contract whose verdict
  this package formats
- `go/internal/answerguardrail/README.md` -- the publish-safety screen the
  scoring criterion calls
- `go/cmd/eshu/answer_quality_scorecard_cmd.go` -- the cobra wrapper showing
  how the two halves fit together
