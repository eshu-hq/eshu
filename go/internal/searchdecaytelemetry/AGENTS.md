# AGENTS.md - internal/searchdecaytelemetry guidance for LLM assistants

## Read first

1. `go/internal/searchdecaytelemetry/README.md` - package purpose and
   boundaries.
2. `go/internal/searchdecay/README.md` - decay policy and scoring contract.
3. `go/internal/telemetry/README.md` - metric naming and label rules.
4. `docs/public/reference/search-decay-scoring.md` - public decay policy
   contract.

## Invariants this package enforces

- **Bridge only** - implement adapters between `searchdecay.Observer` and
  `telemetry.Instruments`; do not add scoring policy, storage, graph, HTTP, or
  MCP behavior here.
- **Low-cardinality labels** - only policy id, evidence class, and outcome are
  metric labels. Evidence ids, graph handles, repository ids, and service ids
  stay out of metrics.
- **Ranking metadata only** - this package must not turn decay scores into
  canonical graph truth or hide evidence.

## Common changes and how to scope them

- **Change counter labels** - update telemetry contract constants, public docs,
  and tests before implementation.
- **Change observer behavior** - add or update `observer_test.go` first and keep
  nil-instrument behavior no-op.

## Anti-patterns specific to this package

- Importing storage, graph, query, MCP, or reducer packages.
- Recording unbounded identifiers as metric labels.
- Adding decay policy decisions that bypass `go/internal/searchdecay`.
