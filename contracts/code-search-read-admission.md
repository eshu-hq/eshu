# CodeSearchReadAdmission v0 (core pin record)

Refs eshu-hq/eshu#9669. W4 search-family core pin, per the arbiter W4
ruling (APPROVED-WITH-CONDITIONS): TWO new unwired v0 pins plus a
semantic record-only cross-reference, pin-first only, no termination,
no tracker boxes. Minted core-side; the satellite records this pin in a
follow-up. A satellite PR consuming an unminted pin is invalid. Arbiter
review of this pin precedes satellite termination of the code search
legs.

## Shape

Version: `CodeSearchReadAdmissionContractVersion = "v0"`
(go/internal/query/code_search_read_admission.go).

Ceiling 9 re-declared (never embedded) methods, sealed — `RunSingle`
is OUT by construction, and no point, list (beyond by-ID hydration),
ledger/summary/snapshot surface rides along:

| # | Method | Mirrors | Serves |
|---|--------|---------|--------|
| 1 | `Run(context.Context, string, map[string]any) ([]map[string]any, error)` | `querycontract.GraphQuery` | `POST /api/v0/code/search` (repo-selected graph lane); `POST /api/v0/code/symbols/search` (content-absent graph lane) |
| 2 | `SearchEntityNames(context.Context, entity.EntityNameSearch) ([]EntityContent, error)` | `*ContentReader` (go/internal/query/content_reader_entity_names.go) | `POST /api/v0/code/search` (global index, no repo) |
| 3 | `SearchSymbols(context.Context, codequery.SymbolSearchRequest) ([]EntityContent, error)` | `*ContentReader` (go/internal/query/content_reader_symbol_search.go) | `POST /api/v0/code/symbols/search` (symbol-aware read) |
| 4 | `SearchCodeCandidates(context.Context, string, string, string, int, bool) ([]EntityContent, []EntityContent, error)` | `*ContentReader` (go/internal/query/content_reader_entity_search.go) | `POST /api/v0/code/search` (repo + language/exact) |
| 5 | `SearchEntitiesByName(context.Context, string, string, string, int) ([]EntityContent, error)` | `*ContentReader` (go/internal/query/content_reader.go) | `POST /api/v0/code/search` (repo fallback); `POST /api/v0/code/symbols/search` (plain-name fallback) |
| 6 | `SearchEntityContent(context.Context, string, string, int) ([]EntityContent, error)` | `*ContentReader` (go/internal/query/content_reader_entity_search.go) | `POST /api/v0/code/search` (repo fallback) |
| 7 | `SearchEntitiesByNameAnyRepo(context.Context, string, string, int) ([]EntityContent, error)` | `*ContentReader` (go/internal/query/content_reader_names.go) | `POST /api/v0/code/search` (corpus-wide fallback branch) |
| 8 | `SearchEntityContentAnyRepo(context.Context, string, int) ([]EntityContent, error)` | `*ContentReader` (go/internal/query/content_reader_names.go) | `POST /api/v0/code/search` (corpus-wide fallback branch) |
| 9 | `ListRepoEntitiesByIDs(context.Context, string, []string, int) ([]EntityContent, error)` | `*ContentReader` (go/internal/query/content_reader_entities_by_ids.go) | `POST /api/v0/code/search` (graph-enrichment hydration) |

There is deliberately no `WithReadStore` constructor on this pin: the
code search seam spans two backends (the concurrent-safe graph reader
plus the guarded Postgres content reader) and minting one constructor
would pick a wiring the satellite never runs. The satellite keeps
building both backends the way `cmd/api` wiring does today; the pin
freezes only the read shape the handlers may call through. The
compile-time witness assertion in the pin file
(`var _ CodeSearchReadAdmission = codeSearchReadAdmissionWitness{}`,
embedding `querycontract.GraphQuery` plus `*ContentReader`) proves the
pair the handlers run against today satisfies the pin.

Enforcement (all in go/internal/query):

- `code_search_read_admission_contract_test.go` — frozen method-set
  test plus version pin
  (`TestCodeSearchReadAdmissionContractIsVersioned`), seal
  (`TestCodeSearchReadAdmissionWireShapesAreSealed` — exactly 9
  methods, no `RunSingle`/ledger/summary/snapshot/point/list
  surface), and the seeded-drift RED half
  (`TestCodeSearchReadAdmissionSeededDriftIsDetected` — the planted
  `RunSingle` widening must not match the frozen set).
- `code_search_read_admission_routes_test.go` — handler tests for
  both routes through the real `codequery` mux with pin-half doubles
  (graph lane + by-ID hydration, global index with grant threading,
  candidate and fallback content reads, symbol-aware read and its
  plain-name fallback, content-absent graph lane), the
  route-level OUT proof
  (`TestCodeSearchReadAdmissionNeverRunsSingle` — no `RunSingle`
  across the served routes), and the route/spec-fragment parity test
  (every pinned path in the OpenAPI code fragments and served by
  `Mount`).

## Census (per route per method)

| Method | `code/search` | `symbols/search` |
|---|---|---|
| `Run` | repo-selected graph lane | content-absent graph lane |
| `SearchEntityNames` | global index (no repo; scoped callers thread the grant) | — |
| `SearchSymbols` | — | symbol-aware read |
| `SearchCodeCandidates` | repo + language/exact | — |
| `SearchEntitiesByName` | repo fallback | plain-name fallback (no symbol seam) |
| `SearchEntityContent` | repo fallback | — |
| `SearchEntitiesByNameAnyRepo` | corpus-wide fallback branch (unscoped, no repo) | — |
| `SearchEntityContentAnyRepo` | corpus-wide fallback branch (unscoped, no repo) | — |
| `ListRepoEntitiesByIDs` | graph-enrichment hydration | — (nil-store enrich is a no-op) |

Reachability notes, recorded not silently assumed: the two corpus-wide
twins live in the shared content-fallback helper's unscoped branch
(`searchEntityContentWithExact`); `code/search` routes the no-repo
shape to the entity-name global index first, so the route subtests pin
the helper's reachable single-repo shapes while the census keeps the
corpus-wide twins IN as the helper's unscoped branch. The symbols
graph lane runs only when the handler carries no content store; with a
store present the symbol-aware read (or its plain-name fallback at
offset 0) answers first. The enrichment hydration runs behind the
code/search graph lane whenever a graph row lacks metadata.

Every other content `AnyRepo` shape is OUT: `SearchFileContentAnyRepo`
and `SearchFileContentAnyRepoExactCase` (file legs belong to the
Content search pin), `SearchEntitiesByExactName` /
`SearchEntitiesByExactNameAnyRepo` (resolve-only helpers no search
route reaches), and `SearchFileReferenceAnyRepo` (reference indexes no
search route reaches).

## Explicitly OUT (not widened by this pin)

- `RunSingle` — the single-row graph read; complexity and
  relationship single-row reads belong to other families' pins. Proven
  at route level, not just in the frozen set.
- File-content search legs, the paged file/entity seams, the unscoped
  walks, and the Dockerfile-install and PHP-config entity pages — the
  Content search pin covers them.
- Point reads (`GetFileContent`, `GetEntityContent` and the twins) —
  the D13 point pin covers them.
- Exact-name twins, component-reference and language/type searches,
  in-repository and multi-get hydration, and every other `List*` leg
  — other families' standing, unchanged.
- Grant checks (`codeGrantAccessFilter`, `codeContentGrantScope`), the
  repository selector, fixed failure messages, the hybrid rerank, the
  semantic overlay (`entitysemantics` summaries), and `QueryProfile`
  (handler behavior, not port shape). SemanticSearch v0 is a
  record-only cross-reference: no semantic method enters this pin and
  nothing semantic is touched.
- Ledger / stored-summary / snapshot surface (none on this family;
  sealed out regardless).

## Satellite consumption

Depend on the shape, not a constructor: serve the two routes' reads
through the existing graph reader (satisfying `Run`) plus the guarded
Postgres content reader (satisfying the eight content reads), the same
pair `cmd/api` wiring builds today. A satellite PR consuming this pin
for any other backend pair is invalid without arbiter review.
