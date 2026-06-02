# AGENTS.md — internal/searchdocs guidance for LLM assistants

## Read first

1. `go/internal/searchdocs/README.md` — package purpose, boundaries, and
   invariants.
2. `go/internal/searchdocs/project.go` — projection contract and exclusion
   rules.
3. `docs/internal/design/430-nornicdb-graph-search-split.md` — parent design
   for separating canonical graph storage from curated search projection.
4. `docs/public/reference/search-document-projection.md` — public contract for
   document shape, source matrix, and telemetry requirements.

## Invariants this package enforces

- **Derived, not canonical** — projected documents are retrieval candidates.
  They must not assert canonical graph truth, ownership, deployment cause, or
  health.
- **Stable handles required** — included documents must have stable graph
  handles so future retrieval can expand only bounded candidates.
- **Sensitive/noisy sources excluded** — raw provider payloads, log lines, trace
  spans, dashboard JSON, query bodies, finding bodies, credentials, secrets, and
  high-cardinality noise stay out of documents.
- **Pure projection** — this package has no I/O, no Postgres dependency, no
  graph dependency, no NornicDB dependency, and no telemetry side effects.

## Common changes and how to scope them

- **Add a new source kind** — add a `SourceKind` constant, a projection input
  type or helper, positive/negative/ambiguous tests, and update
  `docs/public/reference/search-document-projection.md`.
- **Change exclusion behavior** — add a focused regression test first. Prove a
  useful document still projects while the sensitive/noisy row is dropped.
- **Add persistence or API use** — do not add it here. Create a caller in the
  owning storage, reducer, query, or benchmark package and keep this package as
  pure projection logic.

## Failure modes and how to debug

- Symptom: a useful row is skipped — inspect `Decision.Reason` and confirm the
  input carries a stable source id plus repo or runtime handle.
- Symptom: a sensitive value appears in a document — add a red test under
  `project_test.go`, then tighten the source-kind or context exclusion rule.
- Symptom: retrieval wants to expand the whole graph — the document is missing
  a stable `GraphHandle`; fix the source projection instead of widening graph
  search.

## Anti-patterns specific to this package

- Adding database queries, graph writes, or NornicDB calls.
- Treating document rank or similarity as truth.
- Letting source-kind allowlists drift from the public projection contract.
