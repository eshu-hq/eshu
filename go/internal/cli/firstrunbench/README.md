# firstrunbench

## Purpose

Scoring engine for `eshu first-run-benchmark`. It grades a captured
`eshu first-run --json` envelope against the issue #1772 onboarding criteria:
did a new user reach one useful, evidence-backed answer, or did the run only
look healthy? Extracted from `go/cmd/eshu` under #6059 so the grading logic is
importable and unit-testable outside `package main`.

## Ownership boundary

This package owns criterion evaluation, scorecard rendering, and fetching the
artifact bytes (`ReadEnvelope`). Envelope decoding is NOT owned here: the
canonical `{data, truth, error}` contract and its parser live in
`internal/cli/firstrun` (`firstrun.Envelope`, `firstrun.ParseEnvelope`). This
package also does not own cobra flags, stream resolution, environment reads,
or exit-code mapping — those stay in the wrapper
`go/cmd/eshu/first_run_benchmark_cmd.go` — nor the first-run command itself,
the envelope's emit side, or the evidence report.

## Exported surface

`ReadEnvelope` fetches the artifact bytes. `Evaluate` grades a
`firstrun.Envelope` plus `Measurements` into a `Verdict` of `Criterion` rows
(`Verdict.Criterion`, `Verdict.FailureReasons` read it back). `RenderVerdict`
and `Marker` print the human scorecard. The criterion vocabulary
(`CriterionName`, `CriterionStatus`, and their constants) and
`NotMeasuredManualSteps` are exported because the demo-benchmark family in
`go/internal/cli/demo` scores its own envelope with the same vocabulary. See
`doc.go` for the godoc-rendered contract.

## Dependencies

`internal/cli/firstrun` for the envelope contract and the empty-value
placeholder (`firstrun.QuoteIfEmpty`). No cobra, no environment reads
(`go list -deps ./internal/cli/firstrunbench | rg spf13` is empty).

## Telemetry

None. Pure decode/score/render helpers; the CLI process boundary reports the
outcome through its exit code.

## Gotchas / invariants

- Health-only rejection: every required criterion must pass; readiness or
  process health alone never scores as a first answer.
- The envelope is decoded once, by `firstrun.ParseEnvelope`; this package
  scores whatever that decode accepted. Decode-strictness rules (a mistyped
  block fails the parse) are enforced and tested in `internal/cli/firstrun`.
- Not-measured is honest: unset elapsed time or an undeclared manual-step
  count records `not_measured`, never a fabricated value, and never fails an
  otherwise-complete run.

## Related docs

Issue #1772 defines the criteria; #6059 tracks the `cmd/eshu` extraction
series.
