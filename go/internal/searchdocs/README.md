# Searchdocs

## Purpose

`searchdocs` defines the curated search-document contract for Eshu's search
lane. It projects already-indexed content and bounded read-model summaries into
documents that can feed BM25, vector, or hybrid retrieval without turning the
canonical graph into the search corpus.

## Ownership boundary

This package owns pure projection helpers and value types for
`EshuSearchDocument`-shaped records. It does not query Postgres, write graph
state, call NornicDB, expose API/MCP routes, or decide canonical truth.
Collectors remain source-fact emitters, and reducers/query read models own any
future search-document persistence.

## Exported surface

See `doc.go` for the godoc-rendered package contract.

- `Document` is the curated search-lane record.
- `ProjectContentEntity`, `ProjectContentFile`, and `ProjectRuntimeSummary`
  project first-slice inputs into documents.
- `Decision` records whether a candidate was included or excluded.
- `SourceKind`, `TruthScope`, `Freshness`, `AccessScope`, and `Provenance`
  preserve the retrieval contract fields later benchmark and API work must
  honor.

## Dependencies

Standard library only. The package is intentionally a leaf so content,
reducer, query, and benchmark code can consume it without introducing storage or
backend coupling.

## Telemetry

None directly. Future persistence or benchmark callers must emit document
count, skipped-document count, redaction/drop reason, projection duration, and
failure class, as documented in
`docs/public/reference/search-document-projection.md`.

## Gotchas / invariants

- Documents are derived evidence. Search score, vector similarity, and link
  prediction must not become canonical graph truth through this package.
- Every included document must carry at least one stable graph handle for later
  bounded graph expansion.
- Projection drops sensitive context and excluded source kinds. Do not add raw
  provider payloads, log lines, trace spans, dashboard JSON, query bodies,
  finding bodies, credentials, secrets, or high-cardinality noise.
- Output labels are lower-cased, sorted, and deduplicated for deterministic
  fixture and benchmark behavior.

## Related docs

- `docs/internal/design/430-nornicdb-graph-search-split.md`
- `docs/public/reference/search-document-projection.md`
- `docs/public/reference/truth-label-protocol.md`
