# AGENTS.md — internal/correlation guidance for LLM assistants

## Read first

1. `go/internal/correlation/README.md` — pipeline position, ownership
   boundary, and exported surface
2. `go/internal/correlation/observability.go` — `Summary` and `BuildSummary`;
   the only two exported symbols in this package
3. `go/internal/correlation/engine/README.md` — engine.Evaluate is the actual
   evaluation entry point; the root package does not orchestrate evaluation
4. `go/internal/correlation/model/README.md` — candidate state and rejection
   reason constants that `BuildSummary` branches on
5. `CLAUDE.md` "Correlation Truth Gates" section — mandatory reading before
   any change to correlation behavior

## Invariants this package enforces

- **Root does not evaluate** — `correlation` has no evaluation, admission, or
  rendering logic. The correct entry point for running evaluation is
  engine.Evaluate. Do not add evaluation calls here.
- **Summary is a pure reduction** — `BuildSummary` walks `Evaluation.Results`
  once and aggregates. It does not validate, re-sort, or mutate the
  evaluation. Adding side effects to `BuildSummary` breaks callers that treat
  it as a reporting helper.
- **EvaluatedRules is not the pack rule count** — `Summary.EvaluatedRules` is
  `len(evaluation.OrderedRuleNames)`, which comes from the engine's post-sort
  rule list. Adding or removing rules in a pack changes this counter only
  after the engine sorts and emits its ordered names.
- **Multi-reason candidates** — a candidate can hold both rejection reasons
  `low_confidence` and `lost_tie_break`. Both `LowConfidenceCount` and
  `ConflictCount` increment independently.
- **Provisional candidates are skipped** — the state `provisional` is the
  pre-evaluation state. The engine must not emit provisional results; if one
  appears, `BuildSummary` skips it silently (no counter increments).

## Common changes and how to scope them

- **Add a new rejection reason** → add the constant in `correlation/model`,
  add a new counter field to `Summary` in `observability.go`, add the
  corresponding branch in `BuildSummary`. Run
  `go test ./internal/correlation -count=1`. The `observability_test.go`
  test must be updated to cover the new reason.

- **Add a new summary counter** → add the field to `Summary`, populate it in
  `BuildSummary`. Do not add fields that require re-evaluating candidates or
  re-running rules; those belong in engine or admission, not here.

- **Change what reducer reports** → touch the reducer's own status surface
  where it calls `BuildSummary`; the summary shape itself should change only
  when the counter contract changes for all callers.

## Failure modes and how to debug

- Symptom: `Summary.AdmittedCandidates` is always 0 → likely cause: all
  candidates failed the confidence or structural gate → check
  `RejectedCandidates` and `LowConfidenceCount`; if `LowConfidenceCount`
  equals `RejectedCandidates`, the evidence confidence values are below the
  minimum admission confidence threshold for the active pack.

- Symptom: `ConflictCount` > 0 for a deployment that has only one
  candidate → likely cause: two candidates share the same correlation key
  and both passed admission; the engine's tie-break rejected one with
  `lost_tie_break`. Check the correlation key values in the reducer's
  evaluation input for duplicate keys.

- Symptom: `EvaluatedRules` is 0 → likely cause: the rule pack was empty or
  pack validation failed before the engine sorted its rule list → check that
  the active pack passes validation and has at least one rule.

## Anti-patterns specific to this package

- **Adding evaluation logic here** — do not add calls to admission.Evaluate
  or engine.Evaluate to this package. The root is a reporting aggregator;
  evaluation belongs in engine.

- **Counting provisional candidates** — do not add a counter that increments
  when a candidate has state `provisional`. Provisional is a pre-evaluation
  state; the engine should never emit it as a final result, and counting it
  here masks that bug rather than surfacing it.

- **Mutating the Evaluation inside BuildSummary** — `BuildSummary` must not
  sort, filter, or modify `evaluation.Results`. Callers may retain a reference
  to the evaluation after calling `BuildSummary`.

## What NOT to change without an ADR

- `Summary` field names once reducer status surfaces depend on them — renaming
  exported fields breaks JSON serialization in status APIs.
- The split between `ConflictCount` and `LowConfidenceCount` — these map to
  distinct rejection reasons; merging them obscures which gate is failing.
