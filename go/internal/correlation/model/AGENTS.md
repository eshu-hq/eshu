# AGENTS.md — internal/correlation/model guidance for LLM assistants

## Read first

1. `go/internal/correlation/model/README.md` — ownership boundary, exported
   surface, and invariants
2. `go/internal/correlation/model/types.go` — all exported types, constants,
   and `Validate` methods
3. `CLAUDE.md` "Correlation Truth Gates" — mandatory before changing any type
   that flows into materialization truth

## Invariants this package enforces

- **Confidence range** — `Candidate.Confidence` and
  `EvidenceAtom.Confidence` must be in `[0, 1]`. `Validate()` at
  `types.go:78` and `types.go:106` returns an error for values outside this
  range. Callers must not bypass `Validate` when constructing fixtures or
  test candidates.
- **Non-blank required fields** — `ID`, `Kind`, `CorrelationKey` on
  `Candidate`; `ID`, `SourceSystem`, `EvidenceType`, `ScopeID`, `Key` on
  `EvidenceAtom`. `Validate` uses `strings.TrimSpace` checks, so a field
  containing only whitespace is rejected.
- **State machine** — `CandidateStateProvisional` is the pre-evaluation state.
  `CandidateStateAdmitted` and `CandidateStateRejected` are final states set
  by `admission.Evaluate`. No other state transitions exist; the engine must
  not store a provisional candidate as a final engine.Result.
- **RejectionReasons is a slice, not a set** — duplicates are not prevented
  at the model layer. The engine and admission packages are responsible for
  not appending the same reason twice.
- **Value is optional** — `EvidenceAtom.Value` has no non-blank constraint.
  Do not add one without understanding callers that intentionally leave
  `Value` empty.

## Common changes and how to scope them

- **Add a new RejectionReason constant** → add the constant in `types.go`,
  add a branch in `correlation.BuildSummary` (`observability.go`), add
  the appending logic in `engine.Evaluate` (`engine.go`). Run
  `go test ./internal/correlation/... -count=1`. Update
  `observability_test.go` to cover the new reason.

- **Add a new field to Candidate or EvidenceAtom** → add the field to the
  struct; if it must be non-blank, add a `strings.TrimSpace` check in
  `Validate`; if it has a numeric range, add the range check. Run
  `go test ./internal/correlation/... -count=1`. Every downstream package
  that constructs a `Candidate` fixture must be updated.

- **Add a new CandidateState constant** → add the constant in `types.go`,
  add the switch case in `CandidateState.Validate()`. Audit every
  `switch result.Candidate.State` in `engine`, `admission`, and
  `correlation` for the new state.

## Failure modes and how to debug

- Symptom: `admission.Evaluate` returns an error on a candidate you
  constructed → likely cause: `Candidate.Validate()` failed — check that
  all required string fields are non-blank and `Confidence` is in `[0, 1]`.

- Symptom: engine emits a candidate with `CandidateStateProvisional` in
  the final evaluation results → this is a pipeline bug; `admission.Evaluate`
  must set state to `CandidateStateAdmitted` or `CandidateStateRejected`.
  Check the `admission.Evaluate` call path in `engine.go`.

- Symptom: `RejectionReasons` has duplicate entries → admission or engine
  added the same reason twice; check the appending logic in `engine.go` for
  double-append on the same rejection path.

## Anti-patterns specific to this package

- **Bypassing Validate in fixtures** — test helpers that construct `Candidate`
  or `EvidenceAtom` without calling `Validate` can hide contract violations.
  Call `Validate()` in test setup and assert `err == nil`.

- **Adding evaluation logic here** — this package must remain pure data.
  Do not add rule matching, confidence computation, or state-transition logic
  to `types.go`. Those belong in `admission` and `engine`.

- **Reusing RejectionReason as a structured error type** — `RejectionReason`
  is a string constant for operator-facing display. Do not define error
  wrapping or sentinel comparisons on it.

## What NOT to change without an ADR

- `CandidateState` string values once they are persisted in the graph or
  status store — changing the wire value requires a migration.
- `RejectionReason` string values once they appear in explain output, API
  responses, or structured logs — operators and tooling pattern-match on them.
- `Candidate.CorrelationKey` semantics — this is the tie-break domain key;
  changing what it means breaks the engine's winner-selection logic and
  materialization grouping.
