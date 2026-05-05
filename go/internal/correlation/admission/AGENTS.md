# AGENTS.md — internal/correlation/admission guidance for LLM assistants

## Read first

1. `go/internal/correlation/admission/README.md` — gate logic, invariants,
   and ownership boundary
2. `go/internal/correlation/admission/admission.go` — `Evaluate`,
   `satisfiesRequirements`, `matchesRequirement`, `evidenceFieldValue`
3. `go/internal/correlation/rules/README.md` — evidence requirements and
   selectors; understand `MatchAll` conjunctive semantics
4. `go/internal/correlation/engine/README.md` — how the engine uses `Outcome`
   to append rejection reasons; admission does not append them
5. `CLAUDE.md` "Correlation Truth Gates" — mandatory before any change that
   affects which candidates are admitted

## Invariants this package enforces

- **Threshold range** — `threshold` must be in `[0, 1]`. `Evaluate` returns
  an error immediately for out-of-range values (`admission.go:19`). Do not
  relax this check.
- **Returns a copy** — `Evaluate` copies the input candidate into `evaluated`
  at `admission.go:31` and modifies only the copy. Callers must use the
  returned model.Candidate, not their original variable.
- **Does not append rejection reasons** — `Outcome.MeetsConfidence` and
  `Outcome.MeetsStructure` communicate which gates failed, but rejection
  reasons are not modified here. The engine appends them after calling
  `Evaluate`. Do not add reason-appending logic to this package.
- **Empty requirements mean structural pass** — an empty `requiredEvidence`
  slice produces `MeetsStructure = true` (`admission.go:44`). This is
  intentional: a pack with no structural requirements imposes no structural
  gate.
- **Conjunctive MatchAll** — `matchesRequirement` at `admission.go:59` returns
  false as soon as one selector fails to match the atom. All selectors must
  pass for the atom to count toward `MinCount`.
- **Exact string match** — `evidenceFieldValue` returns the raw field string;
  comparisons at `admission.go:61` are exact string equality. No normalization.

## Common changes and how to scope them

- **Add a new evidence field dispatch** → add the case to
  `evidenceFieldValue` (`admission.go:68`). Without the dispatch, selectors
  on the new field will match the empty string and never satisfy a non-blank
  selector value. Run `go test ./internal/correlation/admission -count=1`.

- **Change matching semantics** (e.g., prefix match) → `matchesRequirement`
  at `admission.go:59` is the dispatch point. Document the new semantics;
  do not silently change behavior for existing packs. This requires an ADR
  because it changes the admission contract for all packs.

- **Add a third admission gate** (e.g., maximum evidence count) → add a new
  field to `Outcome`, compute it in `Evaluate`, return it. The engine then
  decides which rejection reason to append. Run
  `go test ./internal/correlation/... -count=1` to ensure all callers of
  `Outcome` are updated.

- **Change threshold validation** → the `[0, 1]` constraint is enforced at
  `admission.go:19`. Changing the allowed range affects all packs. This
  requires updating `rules.RulePack.Validate` in `rules/schema.go` to match.

## Failure modes and how to debug

- Symptom: candidates are rejected with structural mismatch when you expect
  them to pass → call `Evaluate` directly in a test with the candidate and
  requirements; log `Outcome.MeetsStructure` and trace through
  `satisfiesRequirements`. Confirm the evidence atom field values exactly
  match the selector values (case-sensitive, no whitespace tolerance).

- Symptom: `Evaluate` returns an error on a valid candidate → check the
  candidate `Validate` error message; check each requirement `Validate` for
  issues. Common causes: blank candidate ID field, confidence outside `[0, 1]`,
  `MinCount <= 0`, blank selector value.

- Symptom: `MeetsStructure = false` even though evidence atoms look correct →
  check `evidenceFieldValue` dispatch for the fields used in selectors. An
  unknown field returns an empty string, which never satisfies a non-blank
  selector value.

- Symptom: both `MeetsConfidence` and `MeetsStructure` are true but the
  returned state is rejected → this should not happen; `admission.go:35`
  sets the state to admitted when both are true. Check whether a subsequent
  engine step (tie-break) is setting the state back to rejected.

## Anti-patterns specific to this package

- **Appending rejection reasons here** — the engine owns reason appending. Do
  not add candidate rejection reason slice append calls in `Evaluate` or
  helper functions. The split is intentional: `Outcome` is the signal;
  the engine decides what to record.

- **Normalizing selector values** — do not add case-folding or whitespace
  trimming to selector comparison. Evidence atoms are emitted by collectors
  with specific conventions; normalizing comparison silently accepts evidence
  that does not match those conventions.

- **Adding heuristics based on evidence field patterns** — do not add logic
  that infers platform, environment, or cluster from evidence key or value
  string patterns. CLAUDE.md explicitly forbids inventing environment truth
  from heuristic patterns.

- **Mutating the input candidate** — `Evaluate` creates a copy. Do not assign
  to fields on the original `candidate` parameter.

## What NOT to change without an ADR

- `Outcome` field names once callers use them to append rejection reasons —
  renaming `MeetsConfidence` or `MeetsStructure` requires updating the engine
  and every test that checks `Outcome`.
- The conjunctive (MatchAll) semantics of evidence requirements — changing
  to disjunctive semantics changes which candidates pass structural checks
  across all packs.
- The exact-string-comparison behavior in `matchesRequirement` — pack authors
  write selector values assuming exact matching; introducing normalization
  changes which evidence satisfies which requirement.
