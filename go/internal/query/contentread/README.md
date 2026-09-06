# Content-read query handlers

## Purpose

Serves the content read surface: exact file and entity lookups, bounded
file/entity content search over the Postgres content store, and the optional
bounded hybrid (BM25+vector) re-rank of lexical results. All routes hang off
`ContentHandler.Mount` (`POST /api/v0/content/files/read`,
`.../files/lines`, `.../entities/read`, `.../files/search`,
`.../entities/search`).

## Ownership boundary

This package owns everything under `GET/POST /api/v0/content/*`: the handler
struct, the search-request plumbing (`contentSearchRequest`), the
single-entity repository-access helper, the search page bounds, and the
`ContentHybridRanker`. It does not own the `ContentReader` store
implementation or any `content_reader_*.go` file -- those stay in root
package `query` deliberately (see Gotchas below). It does not own auth, the
graph/content port definitions, selector resolution, or the
response-envelope contract -- those live in `querycontract`,
`queryselector`, and the other leaf packages under `internal/query` (see
Dependencies).

Root package `query` keeps compatibility aliases and forwarders
(`content_read_alias.go`) for `ContentHandler`, `ContentHybridRanker`,
`ContentResultReranker`, `FileContent`, `NewContentHybridRanker`, and the
three search page bounds, so `cmd/api` and `cmd/mcp-server` build unchanged.
Root also keeps the OpenAPI `content` path constants and the capability
registrations -- they stay in root deliberately, since root owns the router
and always links into the production binary.

## Exported surface

- `ContentHandler` and `Mount` -- the HTTP entry point. `Content` takes any
  `querycontract.ContentStore`; `HybridRanker` takes any
  `ContentResultReranker`.
- `ContentResultReranker`, `ContentHybridRanker`, `NewContentHybridRanker` --
  the bounded hybrid re-rank (see Gotchas below).
- `FileContent` -- alias onto `querycontract.FileContent`.
- `ContentSearchDefaultLimit/MaxLimit/MaxOffset` -- the search page bounds
  root's OpenAPI sweep test asserts the emitted schema against.
- `querycontract.PagedContentSearcher` (in `querycontract`, not here) -- the
  fast-path interface the handler asserts before falling back to the
  per-request search loop.

## Dependencies

The Go standard library, `database/sql` via the store port, and these
`internal/query` leaf packages:

- `querycontract` -- `ContentStore`, `FileContent`/`EntityContent`,
  `RepositoryAccessFilter` + `RepositoryAccessFilterFromContext`,
  response/truth envelopes, `WriteContentSubstringIndexUnavailable`,
  `ErrContentSubstringIndexesNotReady`, `PagedContentSearcher`.
- `queryselector` -- `ResolveExactForAccess` + `IsNotFound`, the
  repository-selector resolution the search/file/entity paths use.
- `codemodel` -- `EntityIDFromDocument`, the document-ID recovery the
  entity re-rank passes as its rank function (a direct leaf call; root's
  same-named shim in lane-A code stays untouched).
- `queryauth`, `querytestutil` -- test-only: scoped auth contexts and the
  shared `FakePortContentStore` double the tripwire fake embeds.

It does **not** import root package `query`: that import would cycle, since
root imports this package for the compatibility aliases above.

## Telemetry

No new metrics or logs were added by the move itself. The handler emits the
same `WriteSuccess`/`WriteError` envelopes (now via `querycontract`) under
the unchanged `code_search.content_search` capability and
`content_index` truth basis, so existing dashboards are unaffected.

## Gotchas / invariants

**`ContentReader` stays in root, and so does every `content_reader_*.go`
file.** `ContentReader` is shared store infrastructure, not handler code:
sibling-lane files (`cloud_inventory_*`, `documentation_*`, `evidence_*`,
`repository_*`, `semantic_*`, `service_*`, `code_*`) define
`(cr *ContentReader)` read-model methods on it, and it satisfies 13 sibling
interfaces pinned by root's interface-export tripwire. Go requires methods
to live in their type's package, so moving the type would drag sibling
lanes with it. Do not "complete" this move by pulling those files in.

**The paged-search seam is `querycontract.PagedContentSearcher` with
decomposed primitive parameters.** An unexported request type or a
root-local interface could never be satisfied across this boundary -- Go
qualifies unexported names by declaring package, so the assertion would
compile, miss, and silently take the slower loop. Keep the parameters
primitives; reintroducing a shared request struct reopens the silent
fallback. Both sides of the seam are pinned: root's compile-time assertion
in `content_reader.go` and `paged_searcher_tripwire_test.go` here.

**`firstNonEmptyContentRepoID` is a deliberate local copy of root's
`firstNonEmpty`.** An unexported helper cannot be called across a package
boundary. It must stay trivial so the two cannot drift.

**`selectorAwareContentStore` and `contentSubstringIndexNotReadyStore` in
tests are local copies of root doubles**, for the same reason (Go never
compiles one package's `_test.go` into anything another package can
import). Both cite the root original they mirror. The tripwire fake instead
embeds the shared `querytestutil.FakePortContentStore` -- never redeclare
what `querytestutil` already exports.

**The batch entity-access helpers stay in root**
(`content_entity_access_batch.go`). Only the single-entity helper moved
here; the batch path filters through the evidence family's
`filterEvidenceCitationEntitiesForAccess`, so moving it would couple this
package to a sibling lane.

**The hybrid ranker never egresses.** `ContentHybridRanker` embeds a
process-local deterministic hash embedder and refuses any provider
embedder: re-rank must never POST source snippets externally, block on a
provider timeout, or bypass the semantic-policy path. `applied=false`
MUST keep the lexical order and the `content_index` truth basis.

## Related docs

- [HTTP API reference](../../../../docs/public/reference/http-api.md)

## Performance and observability evidence

No-Regression Evidence: this package is a relocation of the content-read
handler family out of `go/internal/query`'s flat root (#6060). No query
text changed, so there is no before/after latency to report and none is
claimed. The emitted SQL is byte-identical across the move: every
`searchFileContentScoped`/`searchEntityContentScoped` call site in root
receives the same WHERE fragment, limit (`request limit + 1` probe), and
offset as before -- the decomposition only changed how the already-computed
values travel from handler to store. Verified by the suite below, which
drives the real SQL shapes through recording fakes and asserts exact
arguments.

No-Observability-Change: the envelopes, capability string, and truth basis
an operator sees are the same ones root emitted (`WriteSuccess`/`WriteError`
now via `querycontract`, identical scope name and attributes). No metric
was added, renamed or removed, and no log key changed.

Why the change is safe: `go test ./internal/query/...` passes, which is
the load-bearing proof -- every external `query.<Type>` reference still
resolves through root's forwarders with no caller edited, and the paged
fast-path tripwire still observes the fast path taken. The B-7 golden
corpus and the B-12 snapshot are untouched
(`git diff --name-only <base>..HEAD -- testdata/` returns nothing), so
projected graph truth and query response shapes are unchanged by
construction rather than by assertion (verified with
`git status --short testdata/` clean plus the B-7 gate passing).
