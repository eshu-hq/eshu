# AGENTS.md — internal/correlation/engine guidance for LLM assistants

## Read first

1. `go/internal/correlation/engine/README.md` — lifecycle, sort contracts,
   tie-break logic, and invariants
2. `go/internal/correlation/engine/engine.go` — `Evaluate`, `admitWinners`,
   `compareCandidates`, `boundedMatchCount`; read the full file before
   changing any sort order
3. `go/internal/correlation/admission/README.md` — what admission.Evaluate
   returns and does NOT do (does not append rejection reasons)
4. `go/internal/correlation/model/README.md` — candidate state and rejection
   reason constants
5. `CLAUDE.md` "Correlation Truth Gates" — mandatory before any change that
   affects which candidates are admitted

## Invariants this package enforces

- **DETERMINISM: rule sort** — `Evaluate` clones the pack's `Rules` slice and
  sorts by `(Priority ascending, Name ascending)` with `slices.SortFunc` at
  `engine.go:31`. Do not remove or reorder this sort. `OrderedRuleNames`
  reflects the post-sort order; callers and tests depend on it.
- **DETERMINISM: result sort** — results are sorted by
  `(CorrelationKey ascending, admitted-before-rejected, ID ascending)` with
  `slices.SortFunc` at `engine.go:71`. This ordering is part of the public
  contract for explain rendering and replay.
- **DETERMINISM: tie-break** — `admitWinners` at `engine.go:92` uses
  `compareCandidates` at `engine.go:115`: higher `Confidence` wins; equal
  `Confidence` resolves to lower lexicographic `ID`. The loser receives
  rejection reason `lost_tie_break`. Do not change this ordering without an ADR.
- **No partial results** — `Evaluate` returns `(Evaluation{}, err)` on the
  first validation error. Callers must not treat an empty `Evaluation` as a
  valid empty result.
- **Rejection reasons appended here, not in admission** — admission.Evaluate
  returns an outcome but does not append rejection reasons. The engine appends
  `low_confidence` at `engine.go:57` and `structural_mismatch` at
  `engine.go:60`. Do not move reason appending into admission.
- **MatchCounts only for RuleKindMatch** — the `MatchCounts` loop at
  `engine.go:51` skips non-match rules with `continue`. Adding a new rule
  kind that should produce match counts requires an explicit case here.

## Common changes and how to scope them

- **Add a new rejection reason** → add the constant in `correlation/model`,
  identify when in the evaluation loop the reason should be appended, add the
  append call in `engine.go`, add a branch in `correlation.BuildSummary`.
  Run `go test ./internal/correlation/... -count=1`. Add a test in
  `engine_test.go` that proves the reason appears under the new condition.

- **Change tie-break logic** → touch `compareCandidates` at `engine.go:115`
  and `admitWinners` at `engine.go:92`. Update the tie-break test in
  `engine_test.go` to assert the new winner. Update explain golden tests if
  the candidate order changes. This is a correctness decision — read
  CLAUDE.md "Correlation Truth Gates" first.

- **Add a new result sort key** → touch the `slices.SortFunc` call at
  `engine.go:71`. Ensure the new key is deterministic (not random, not
  map-iteration-order). Update `engine_test.go` tests that assert result order.

- **Support a new rule kind in MatchCounts** → add the kind to the
  `if rule.Kind != rules.RuleKindMatch { continue }` guard at `engine.go:51`.
  Add a test that confirms the count is populated for the new kind.

## Failure modes and how to debug

- Symptom: `Evaluate` returns an error on a valid-looking pack → call
  `pack.Validate()` separately and log the error. Most common causes: empty
  `Rules` slice, `MinAdmissionConfidence` outside `[0, 1]`, blank rule name.

- Symptom: admitted count is 0 when you expect some admissions → check
  admission.Evaluate outcomes directly; log `MeetsConfidence` and
  `MeetsStructure`. Confirm candidate `Confidence` values meet the pack
  threshold.

- Symptom: tie-break winner is unexpected → candidates with equal confidence
  are resolved by lexicographic `ID`. If `ID` values are GUIDs or
  content-addressed hashes, the winner is the one whose ID is alphabetically
  first. Check `compareCandidates` at `engine.go:115`.

- Symptom: `MatchCounts` is empty for a match rule → confirm the rule has
  `Kind == rules.RuleKindMatch`. Non-match kinds are skipped. Confirm the
  candidate's evidence slice is non-empty.

- Symptom: `OrderedRuleNames` does not match what you expect → rules are
  sorted before the names list is built (`engine.go:39-42`). The order comes
  from `(Priority, Name)`, not from the pack constructor's slice order.

## Anti-patterns specific to this package

- **Returning partial results on error** — do not change `Evaluate` to return
  partial results when a candidate fails validation. Callers assume an error
  means no valid evaluation occurred.

- **Sorting rules inside the pack constructor** — pack constructors in rules
  return value types with unsorted slices. The engine sorts at evaluation time.
  Do not pre-sort in constructors to "help" the engine; the sort here is the
  canonical source of order.

- **Moving rejection reason appending into admission** — admission.Evaluate
  returns an outcome without mutation. Appending reasons in admission would
  mean the engine has no opportunity to add multiple reasons cleanly.

- **Adding namespace or folder heuristics** — do not add any logic that infers
  environment, cluster, or platform placement from `CorrelationKey` string
  patterns or evidence key prefixes. CLAUDE.md forbids heuristics that invent
  environment truth.

## What NOT to change without an ADR

- `compareCandidates` tie-break order — changing which candidate wins for a
  given `CorrelationKey` changes materialized graph truth.
- `slices.SortFunc` ordering in `Evaluate` results — explain rendering and
  replay tests depend on stable result order.
- Rule sort key `(Priority, Name)` — pack authors set priorities to control
  order; changing the sort breaks their intent and all golden tests.
- The semantics of `Evaluation.OrderedRuleNames` — callers derive the
  evaluated rule count from its length.
