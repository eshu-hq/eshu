# AGENTS.md - internal/searchretrieval guidance for LLM assistants

## Read first

1. `go/internal/searchretrieval/README.md` - package purpose, boundaries, and
   invariants.
2. `go/internal/searchretrieval/retrieval.go` - request validation and response
   normalization.
3. `go/internal/searchdocs/README.md` - curated search-document projection
   contract.
4. `go/internal/searchbench/README.md` - benchmark scoring and evidence
   contract.
5. `docs/public/reference/search-retrieval-contract.md` - public reference for
   this internal eval path.

## Invariants this package enforces

- **Bounded before backend use** - every request must have a query, one scope
  anchor, limit, timeout, and valid search mode.
- **Curated documents only** - candidates are `searchdocs.Document` records, not
  whole-graph nodes or raw backend rows.
- **Finite ranking scores** - reject `NaN` or infinite candidate scores before
  sorting top-K results.
- **Derived truth only** - non-derived result truth levels are counted as false
  canonical claims. Do not convert them to canonical truth.
- **No I/O** - this package has no Postgres, graph, NornicDB, HTTP, MCP, or
  telemetry side effects.

## Common changes and how to scope them

- **Change request validation** - add a red test under `retrieval_test.go` with
  the exact invalid request, then update `ValidateRequest`.
- **Change ranking behavior** - add a fixture with score ties and document ids.
  Keep deterministic ordering.
- **Add adapter-specific data** - put backend details in adapter packages, not
  in this pure contract, unless every backend needs the same field.

## Failure modes and how to debug

- Symptom: a request is rejected - inspect `ValidateRequest` output for missing
  scope, limit, timeout, query, or mode.
- Symptom: benchmark scores drift - confirm `Response.SearchbenchResults`
  preserves document graph handles and rank.
- Symptom: false canonical claims are nonzero - inspect result document
  `TruthScope.Level`; fix the producer instead of hiding the count.

## Anti-patterns specific to this package

- Adding database clients, graph queries, NornicDB search calls, HTTP handlers,
  MCP tools, or telemetry exporters.
- Accepting unscoped semantic search because a fixture is small.
- Treating score, rank, vector similarity, or link prediction as graph truth.
