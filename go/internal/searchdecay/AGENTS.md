# AGENTS.md - internal/searchdecay guidance for LLM assistants

## Read first

1. `go/internal/searchdecay/README.md` - package purpose, boundaries, and
   invariants.
2. `go/internal/searchdecay/decay.go` - policy validation and scoring.
3. `go/internal/searchdocs/README.md` - derived search-document truth contract.
4. `docs/public/reference/search-decay-scoring.md` - public reference for this
   ranking-metadata contract.

## Invariants this package enforces

- **Canonical truth is not decayed** - canonical graph evidence, admitted durable
  relationships, and non-derived truth labels must skip decay. Missing truth
  labels must be rejected.
- **Ranking metadata only** - decay scores can affect ranking evidence, not graph
  truth or materialization.
- **Low-cardinality observations** - observation dimensions are policy id,
  evidence class, and outcome. Do not add evidence ids as labels.
- **No I/O** - this package has no Postgres, graph, NornicDB, HTTP, MCP, or OTEL
  side effects.

## Common changes and how to scope them

- **Add an eligible class** - add the constant, default policy coverage, tests,
  and public docs.
- **Change scoring math** - add a red test with exact age, half-life, min score,
  and expected bounded score before changing `Scorer.Score`.
- **Change telemetry shape** - keep observations low-cardinality and update docs
  plus tests first.

## Failure modes and how to debug

- Symptom: canonical evidence decays - inspect `isCanonicalEvidence` and truth
  labels; fix the producer or skip rule.
- Symptom: all scores clamp to `Policy.MinScore` - check age, half-life, and
  whether the policy is too aggressive for the evidence class.
- Symptom: telemetry cardinality rises - inspect observation fields for evidence
  ids, handles, repository ids, service ids, or other unbounded values.

## Anti-patterns specific to this package

- Adding database clients, graph queries, NornicDB calls, HTTP handlers, MCP
  tools, or OTEL exporters.
- Treating decay as a filter that hides evidence.
- Applying decay to canonical graph truth or admitted durable relationships.
