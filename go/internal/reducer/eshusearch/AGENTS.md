# Agent instructions: internal/reducer/eshusearch

Scoped rules for this directory. The root `AGENTS.md` still applies.

## What this package is

The curated search-document projection (design 430): `EshuSearchDocumentHandler`
loads a scope's indexed content through `SearchDocumentSourceLoader`, curates it
with `ProjectSearchDocuments`, and persists it through `SearchDocumentWriter` —
in production, `PostgresEshuSearchDocumentWriter`, which also writes the
search-index terms those documents are found by. This is a Postgres read-model
projection; it performs no canonical graph write.

It was extracted from the `internal/reducer` root (issue #6061, epic #6053)
because the root owns domain registration and handler wiring for every family,
so a family living in the root could never be imported by the root.

## Hard rules

**Never import `internal/reducer`**, directly or transitively. The root already
imports this package (registry, defaults wiring, `cmd/reducer`) to register and
construct the domain; the reverse import is a cycle. If you need a root-owned
type, either move it here too or reconsider whether the code belongs in this
package.

**Do not add a compatibility alias for this family's symbols back into the
root.** The move repointed every caller (`internal/reducer`,
`internal/projector`, `internal/storage/postgres`, `cmd/reducer`) to import
`eshusearch` directly instead of leaving root-level forwarders. Reintroducing
one defeats that and reopens the risk of a stale alias drifting from the real
type.

**Finalize is once-per-session, not once-per-page.** `SearchDocumentWriter`
streams pages independently for bounded memory, but the authoritative retire
runs once in `Finalize` over the union of every document seen. If you add a new
call site that inserts pages, route its finalize through the same session
rather than finalizing per page — that silently breaks the union-keep-set retire
and can drop documents.

**Cancel on stream error is not optional.** A `StreamSearchDocumentSources`
error after some pages were already inserted must call `session.Cancel` before
returning, or the scope is left queryable with a partial write (issue #3450 —
this was a review P1, not a stylistic choice).

## Changing `DomainEshuSearchDocument` gating

`defaults_additive_domains_correlation.go` in the root only registers this
domain when both `EshuSearchDocumentSourceLoader` and `EshuSearchDocumentWriter`
are non-nil on `DefaultHandlers`. If you add a new required adapter to
`EshuSearchDocumentHandler`, update that gate too, or a partially-wired
production binary will silently register a domain that panics or no-ops on
every intent instead of not registering at all.

## Postgres surface

`eshu_search_document_writer.go`, `eshu_search_document_writer_queries.go`, and
`eshu_search_document_index_writer.go` hold the SQL this package issues against
`fact_records` and the search-index term tables. Changes to any of those —
predicates, transaction boundaries, index usage — fall under the
`eshu-postgres-rigor` project skill; load it before editing them, same as any
other Postgres surface in this repo.

## Telemetry

`eshu_dp_search_index_mutations_total`, `eshu_dp_search_index_errors_total`,
`eshu_dp_search_index_write_duration_seconds`, plus the shared
`CanonicalWrites`/`CanonicalWriteDuration` instruments tagged with
`DomainEshuSearchDocument`. `docs/public/observability/telemetry-coverage.md`
carries the per-file rows; update the row's path if a file inside this package
is renamed or split further.
