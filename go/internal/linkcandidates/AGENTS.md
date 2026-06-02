# AGENTS.md - internal/linkcandidates guidance for LLM assistants

## Read first

1. `go/internal/linkcandidates/README.md` - package purpose, boundaries, and
   invariants.
2. `go/internal/linkcandidates/candidate.go` - candidate validation contract.
3. `go/internal/linkcandidates/evaluate.go` - relationship-gap evaluation gate.
4. `docs/public/reference/link-prediction-candidates.md` - public reference for
   diagnostic candidate evidence.
5. `docs/public/reference/relationship-mapping.md` - canonical relationship
   stage ownership.
6. `docs/public/reference/relationship-mapping-evidence.md` - resolver-owned
   evidence and materialization path.

## Invariants this package enforces

- **Candidates are not canonical relationships** - this package validates
  diagnostic suggestions only.
- **Reducer admission is separate** - canonical relationship rows require a
  future reducer-owned design.
- **Ambiguous remains provenance-only** - ambiguity is visible and never
  auto-resolved here.
- **Low-cardinality observations** - observation dimensions are algorithm and
  decision only.
- **Evaluation is evidence only** - precision, recall, and false positives
  guide experiments but do not admit relationships.
- **No I/O** - this package has no NornicDB, Postgres, graph, HTTP, MCP, or
  OTEL side effects.

## Common changes and how to scope them

- **Add a decision** - add the constant, validation, positive/negative tests,
  and public docs.
- **Change truth labels** - keep labels non-canonical and update docs before
  code.
- **Change telemetry shape** - keep observations low-cardinality and update
  tests first.
- **Change evaluation scoring** - cover matched gaps, false positives,
  suppressed candidates, ambiguous candidates, and invalid candidate labels.

## Failure modes and how to debug

- Symptom: candidates look canonical - inspect `TruthLevel` and docs; do not
  add `exact`, `derived`, or `canonical` labels here.
- Symptom: telemetry cardinality rises - inspect observation fields for source,
  target, repository, service, evidence, or candidate ids.
- Symptom: ambiguous candidates are treated as edges - move that work out of
  this package and require reducer-owned admission design.

## Anti-patterns specific to this package

- Adding live NornicDB procedure calls.
- Writing graph edges or resolved relationship rows.
- Shaping public API/MCP responses in this package.
- Treating link-prediction score as proof of a dependency.
