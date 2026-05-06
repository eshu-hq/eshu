# AGENTS.md — internal/correlation/rules guidance for LLM assistants

## Read first

1. `go/internal/correlation/rules/README.md` — schema overview, first-party
   pack inventory, and invariants
2. `go/internal/correlation/rules/schema.go` — `RuleKind`, `EvidenceField`,
   `EvidenceSelector`, `EvidenceRequirement`, `Rule`, `RulePack`, and all
   `Validate` methods
3. `go/internal/correlation/rules/container_rulepacks.go` — `ContainerRulePacks`
   and `FirstPartyRulePacks`; understand the split before adding a new pack
4. Any one existing pack file (e.g. `dockerfile_rules.go`) as a structural
   template
5. `CLAUDE.md` "Correlation Truth Gates" — mandatory before touching any pack
   that affects correlation admission

## Invariants this package enforces

- **DETERMINISM: sort is owned by the engine** — `RulePack.Rules` order is
  not the evaluation order. The engine sorts by `(Priority ascending, Name
  ascending)` before evaluating. Pack authors must set priorities to express
  intent; the engine, not the constructor, enforces the order.
- **MinCount must be positive** — `EvidenceRequirement.MinCount <= 0` returns
  an error from `Validate` (`schema.go:93`). Do not set `MinCount` to 0 to
  mean "optional"; omit the requirement instead.
- **MaxMatches = 0 means unbounded** — the engine treats `MaxMatches <= 0` as
  no cap. A value of 0 does NOT mean "disallow."
- **MatchAll is conjunctive** — all selectors in `EvidenceRequirement.MatchAll`
  must match a single atom for it to count. Do not assume OR semantics.
- **MinAdmissionConfidence is the hard gate** — once set in a pack, lowering
  it admits candidates that previously failed. Changing thresholds changes
  which deployable units appear in the graph. Treat as a correctness decision,
  not a tuning knob.
- **Validate must pass before engine consumption** — `engine.Evaluate` calls
  `pack.Validate()` and returns an error if it fails. Invalid packs produce
  no results, not partial results.

## Common changes and how to scope them

- **Add a new first-party rule pack** → create a new `*_rules.go` file with
  one exported constructor function following the pattern in
  `dockerfile_rules.go`. Add it to `FirstPartyRulePacks()` in
  `container_rulepacks.go`. Add to `ContainerRulePacks()` only if the pack
  belongs to the container correlation family. Run
  `go test ./internal/correlation/rules -count=1`. The `schema_test.go`
  validates all packs returned by `FirstPartyRulePacks()`.

- **Change a MinAdmissionConfidence value** → verify that the new threshold
  does not admit or reject candidates that would produce wrong graph truth
  (see CLAUDE.md "Correlation Truth Gates"). Run the full correlation test
  suite: `go test ./internal/correlation/... -count=1`. Document the change
  in the active ADR evidence row.

- **Add a new RuleKind constant** → add to `schema.go`, add the case to
  `RuleKind.Validate()`, add the case to `engine.Evaluate`'s `matchCounts`
  loop if the new kind should populate match counts, and to any other
  switch on `RuleKind` in downstream packages.

- **Add a new EvidenceField constant** → add to `schema.go`, add to
  `EvidenceField.Validate()`, add to `admission.evidenceFieldValue` in
  `admission.go`. Without the admission dispatch, the new field value returns
  an empty string and selectors using it will never match.

- **Add a new EvidenceRequirement to an existing pack** → ensure `MinCount`
  is positive and `MatchAll` has at least one selector. Run golden tests to
  confirm that previously-admitted candidates that lack the new evidence are
  now rejected with reason `structural_mismatch`. This is an admission
  threshold change; treat it as a correctness decision.

## Failure modes and how to debug

- Symptom: `engine.Evaluate` returns an error on a first-party pack → the
  pack is invalid; call `pack.Validate()` directly and check the returned
  error message. Common causes: empty `Rules`, `MinAdmissionConfidence`
  outside `[0, 1]`, blank rule name.

- Symptom: `EvidenceRequirement` is silently never satisfied → check that
  `MinCount` is positive and that the selector field and value exactly match
  what evidence atoms carry. Selector comparison is case-sensitive exact
  string match — there is no normalization.

- Symptom: `ContainerRulePacks` does not include a pack you added to
  `FirstPartyRulePacks` → `ContainerRulePacks` is a separate, explicitly
  enumerated slice. Add the pack to both if it belongs in both families.

## Anti-patterns specific to this package

- **Heuristic namespace or folder selectors** — do not add selectors based on
  repository folder names, namespace prefixes, or repo-name patterns to infer
  environment or platform placement. CLAUDE.md explicitly forbids namespace,
  folder, or repo-name heuristics that invent environment truth.

- **Lowering MinAdmissionConfidence to "fix" admission failures** — a
  confidence gate is the first signal that evidence quality is low. Lowering
  the threshold to admit more candidates hides the root cause. Investigate
  the evidence source first.

- **Putting evaluation logic in constructors** — pack constructors must return
  static `RulePack` values. Do not call `admission.Evaluate` or
  `engine.Evaluate` inside a constructor.

- **Sharing `Rule` slices between packs** — pack constructors return value
  types and compose their own `Rule` slices. Do not declare shared slice
  variables to reuse across packs; the engine sorts in place on a clone, but
  sharing state is a future-maintenance hazard.

## What NOT to change without an ADR

- `MinAdmissionConfidence` values in shipped packs — admission threshold
  changes affect which correlations appear in the graph; treat as a
  correctness decision requiring evidence.
- `EvidenceRequirement.MinCount` and `MatchAll` selectors in shipped packs —
  structural requirements define what evidence must exist for admission;
  changes affect admitted populations.
- `RuleKind` and `EvidenceField` string wire values once they are recorded in
  explain output, status APIs, or persisted state.
- The `ContainerRulePacks` / `FirstPartyRulePacks` split — callers may depend
  on the distinction; removing or merging it changes caller behavior.
