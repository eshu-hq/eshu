# firstrunbench

## Purpose

Scoring engine for `eshu first-run-benchmark`. It decodes a captured
`eshu first-run --json` envelope and grades it against the issue #1772
onboarding criteria: did a new user reach one useful, evidence-backed answer,
or did the run only look healthy? Extracted from `go/cmd/eshu` under #6059 so
the grading logic is importable and unit-testable outside `package main`.

## Ownership boundary

This package owns envelope decoding, criterion evaluation, and scorecard
rendering. It does not own cobra flags, stream resolution, environment reads,
or exit-code mapping — those stay in the wrapper
`go/cmd/eshu/first_run_benchmark_cmd.go`. It also does not own the first-run
command itself, the envelope's emit side, or the evidence report that lifts
`envelope.Data` into a full first-run result; those remain in `go/cmd/eshu`.

## Exported surface

`ParseEnvelope` and `ReadEnvelope` decode and fetch the artifact. `Evaluate`
grades an `Envelope` plus `Measurements` into a `Verdict` of `Criterion` rows
(`Verdict.Criterion`, `Verdict.FailureReasons` read it back). `RenderVerdict`
and `Marker` print the human scorecard. The criterion vocabulary
(`CriterionName`, `CriterionStatus`, and their constants) and
`NotMeasuredManualSteps` are exported because the demo-benchmark family in
`go/cmd/eshu` scores its own envelope with the same vocabulary. See `doc.go`
for the godoc-rendered contract.

## Dependencies

Standard library only. No cobra, no environment reads
(`go list -deps ./internal/cli/firstrunbench | rg spf13` is empty).

## Telemetry

None. Pure decode/score/render helpers; the CLI process boundary reports the
outcome through its exit code.

## Gotchas / invariants

- Health-only rejection: every required criterion must pass; readiness or
  process health alone never scores as a first answer.
- `Envelope`/`Result`/`Step`/`Diagnostic` mirror the wire shape emitted by
  first-run in `go/cmd/eshu`, field for field, so decode strictness matches
  the canonical envelope. The wire-parity test in
  `go/cmd/eshu/first_run_benchmark_cmd_test.go` fails if either side drifts —
  fix the drifted side, not the test. If the in-flight first-run extraction
  lands an importable envelope type, migrate this mirror onto it.
- Not-measured is honest: unset elapsed time or an undeclared manual-step
  count records `not_measured`, never a fabricated value, and never fails an
  otherwise-complete run.
- `n` is a verbatim copy of the empty-value placeholder in
  `go/cmd/eshu/first_run.go` (same pattern as `evidpacket` and
  `answerqualityscorecard`). Do not export it or import it from anywhere.

## Related docs

Issue #1772 defines the criteria; #6059 tracks the `cmd/eshu` extraction
series.
