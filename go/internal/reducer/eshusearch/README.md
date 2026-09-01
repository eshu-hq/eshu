# internal/reducer/eshusearch

## Purpose

Owns the curated search-document projection (design 430). It loads a
repository scope's indexed content (`content_entities`, `content_files`),
curates it into `EshuSearchDocument` records, and writes the derived facts plus
their search index terms into the Postgres search-lane read model. It performs
no canonical graph write — this is a Postgres read-model projection, not a
graph write path.

Extracted from the `internal/reducer` root as part of issue #6061 (epic
#6053): the root registers, wires, and constructs every domain, so a family
that lived in the root could never be imported by the root without a cycle.
This family had exactly six symbols undefined outside the root, all with
existing leaf owners (`internal/reducer/contract`, `internal/reducer/factwrite`,
`internal/reducer/payloadcore`), so it needed no new hoist to become a
subpackage.

## Ownership boundary

**Owns:** the `EshuSearchDocumentHandler` domain handler, the
`SearchDocumentSourceLoader` / `SearchDocumentWriter` interfaces, the curation
projection (`ProjectSearchDocuments`), the production Postgres writer
(`PostgresEshuSearchDocumentWriter`) including its search-index term writing
and observability, and the domain/fact-kind identifiers
(`DomainEshuSearchDocument`, `EshuSearchDocumentFactKind`).

**Does not own:** registration into the reducer's domain registry
(`internal/reducer/registry_additive_domains.go`), adapter wiring into
`DefaultHandlers` (`internal/reducer/defaults_handlers.go`,
`internal/reducer/defaults_additive_domains_correlation.go`), the pending-scope
sweeper that drives the domain's intents (`internal/projector`), or the
Postgres source loader that streams `content_entities`/`content_files` pages
(`internal/storage/postgres`). Those stay where they were; only this family's
own files moved.

## Exported surface

| symbol | what it is |
|---|---|
| `EshuSearchDocumentHandler` | the domain's `Handle` entrypoint |
| `SearchDocumentSourceLoader` | streams source pages for one scope+generation |
| `SearchDocumentWriter` | begins/inserts/finalizes/cancels a curated write session |
| `SearchDocumentProjectionInput` / `ProjectSearchDocuments` | curates one source page into documents |
| `PostgresEshuSearchDocumentWriter` | the production `SearchDocumentWriter`, persisting fact_records plus BM25 search index terms |
| `EshuSearchDocumentWriteBegin` / `EshuSearchDocumentWriteResult` | the begin/finalize shapes for a write session |
| `DomainEshuSearchDocument` | the reducer domain identifier |
| `EshuSearchDocumentFactKind` | the durable `fact_records.fact_kind` value |
| `EshuSearchDocumentWriteTimings` | in-process timing accumulator for one write cycle |

This family kept no compatibility aliases in the reducer root. Every caller —
`internal/reducer` (registry, defaults wiring), `internal/projector` (pending
sweeper), `internal/storage/postgres` (source loader), and `cmd/reducer`
(production wiring) — imports `eshusearch` directly and was repointed in the
same change that moved the files.

## Dependencies

`internal/reducer/contract` for `Intent`, `Result`, and
`ResultStatusSucceeded` (the reducer root only aliases these under its own
names). `internal/reducer/factwrite` for the writer's timestamp
(`Now`) and collector-kind normalization (`CollectorKind`).
`internal/reducer/payloadcore` for `UniqueSortedStrings`. Plus
`internal/facts`, `internal/searchdocs`, `internal/searchhybrid`, and
`internal/telemetry` for fact-row and search-term shapes, and the standard
library / `database/sql` / OpenTelemetry for the Postgres writer and its spans.

**This package must never import `internal/reducer`.** The root imports this
package to register and wire the domain; the reverse import would be a cycle.

## Telemetry

`eshu_dp_search_index_mutations_total`, `eshu_dp_search_index_errors_total`,
and `eshu_dp_search_index_write_duration_seconds` are recorded by
`eshu_search_index_observability.go` from both the fact writer
(`eshu_search_document_writer.go`) and the search-index term writer
(`eshu_search_document_index_writer.go`). The handler
(`eshu_search_document.go`) also records `CanonicalWrites` /
`CanonicalWriteDuration` on the shared reducer instruments, tagged with
`DomainEshuSearchDocument`. `eshu_search_document_write_timings.go` holds an
in-process timing accumulator with no metric of its own — see the
`No-Observability-Change` row for it in
`docs/public/observability/telemetry-coverage.md`. That file's rows for these
three files were repointed to `go/internal/reducer/eshusearch/...` in the same
change that moved them.

## Gotchas / invariants

**Finalize runs once over the union keep-set, not per page.** The handler
streams and inserts source pages independently to keep peak memory bounded to
one page (issue #3440), but the authoritative retire only happens once, in
`Finalize`, over every document seen across all pages. A mid-stream error
triggers `Cancel` to remove the partial pages already inserted so the scope is
never left queryable in a half-written state (issue #3450).

**`ResultStatusSucceeded` and friends come from `reducercontract`, not a local
alias.** Unlike some reducer subpackages, this one does not define its own
`Intent`/`Result` type — it imports `internal/reducer/contract` directly. Don't
reintroduce a root-owned alias here; that would recreate the cycle this move
exists to avoid.

**`DomainEshuSearchDocument` has three separate call sites in the root that
must stay in sync if the domain ever needs new adapter gating**: the registry
definition (`registry_additive_domains.go`), the handler assembly
(`defaults_additive_domains_correlation.go`, gated on both
`EshuSearchDocumentSourceLoader` and `EshuSearchDocumentWriter` being non-nil),
and the adapter field declaration (`defaults_handlers.go`).

## Related docs

- `go/internal/reducer/README.md` — the root package and its subpackage inventory
- `docs/public/reference/search-document-projection.md` — the design-430 search lane read model
- `docs/public/observability/telemetry-coverage.md` — the coverage rows for this package's files
- `docs/internal/design/4784-reducer-derived-fact-governance.md` — the `reducer_eshu_search_document` governance row (file:line citations there predate this move and were not updated by it)
