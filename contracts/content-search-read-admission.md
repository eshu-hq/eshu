# ContentSearchReadAdmission v0 (core pin record)

Refs eshu-hq/eshu#9669. W4 search-family core pin, per the arbiter W4
ruling (APPROVED-WITH-CONDITIONS): TWO new unwired v0 pins plus a
semantic record-only cross-reference, pin-first only, no termination,
no tracker boxes. Minted core-side; the satellite records this pin in a
follow-up. A satellite PR consuming an unminted pin is invalid. Arbiter
review of this pin precedes satellite termination of the content search
legs.

## Shape

Version: `ContentSearchReadAdmissionContractVersion = "v0"`
(go/internal/query/content_search_read_admission.go).

Exactly 10 re-declared (never embedded) methods, sealed — no point, no
list, no ledger/summary/snapshot surface:

| # | Method | Mirrors (*ContentReader) | Serves |
|---|--------|--------------------------|--------|
| 1 | `SearchFiles(context.Context, string, []string, string, int, int) ([]FileContent, error)` | go/internal/query/content_reader_search_page.go | `POST /api/v0/content/files/search` (scoped paged seam) |
| 2 | `SearchEntities(context.Context, string, []string, string, int, int) ([]EntityContent, error)` | go/internal/query/content_reader_search_page.go | `POST /api/v0/content/entities/search` (scoped paged seam) |
| 3 | `SearchFileContent(context.Context, string, string, int) ([]FileContent, error)` | go/internal/query/content_reader.go | `POST /api/v0/content/files/search` (single-repo fallback) |
| 4 | `SearchFileContentAnyRepo(context.Context, string, int) ([]FileContent, error)` | go/internal/query/content_reader_names.go | `POST /api/v0/content/files/search` (corpus-wide fallback) |
| 5 | `SearchEntityContent(context.Context, string, string, int) ([]EntityContent, error)` | go/internal/query/content_reader_entity_search.go | `POST /api/v0/content/entities/search` (single-repo fallback) |
| 6 | `SearchEntityContentAnyRepo(context.Context, string, int) ([]EntityContent, error)` | go/internal/query/content_reader_names.go | `POST /api/v0/content/entities/search` (corpus-wide fallback) |
| 7 | `SearchFilesUnscoped(context.Context, string, int, int, string, string, int) (querycontract.FileSearchPage, error)` | go/internal/query/content_reader_search_page.go | `POST /api/v0/content/files/search` (unscoped bounded walk) |
| 8 | `SearchEntitiesUnscoped(context.Context, string, int, int, querycontract.SearchCursor) (querycontract.EntitySearchPage, error)` | go/internal/query/content_reader_search_page.go | `POST /api/v0/content/entities/search` (unscoped bounded walk) |
| 9 | `SearchDockerfileInstallEntities(context.Context, string, []string, string, string, int, string, int) ([]querycontract.EntityContent, error)` | go/internal/query/content_reader_dockerfile_install.go | `POST /api/v0/content/dockerfile-installs/search` |
| 10 | `SearchPHPConfigEntities(context.Context, []string, string, *querycontract.PHPConfigCursor, int) ([]querycontract.EntityContent, error)` | go/internal/query/content_reader_php_config.go | `POST /api/v0/content/php-config/search` |

Transport: `NewContentSearchReadAdmissionWithReadStore(db.ReadStore,
time.Duration, *slog.Logger, *telemetry.Instruments)` delegates to the
existing `NewContentReaderWithReadStore` and applies `WithInstruments`
plus `WithUnscopedSearch` with the production budget the same way
`cmd/api` wiring does — no parallel wiring, no second place for the
guarded handle, the bounded walk, or operator metrics to be
misconfigured. A constructor that drops the budget fails the
wire-shapes budget test, and one that drops the instruments fails the
instruments assertion in the same test. The fail-closed proof plus the
full wire-object byte mirror against `*ContentReader` covers the
satellite path end to end. Concrete-double-only proof is rejected.

Enforcement (all in go/internal/query):

- `content_search_read_admission_contract_test.go` — frozen method-set
  test plus version pin
  (`TestContentSearchReadAdmissionContractIsVersioned`), seal
  (`TestContentSearchReadAdmissionWireShapesAreSealed` — exactly 10
  methods, no `RunSingle`/ledger/summary/snapshot/point/list surface),
  and the seeded-drift RED half
  (`TestContentSearchReadAdmissionSeededDriftIsDetected` — the planted
  `SearchFileContentAnyRepoExactCase` widening must not match the
  frozen set).
- `content_search_read_admission_wire_shapes_test.go` — wire-shapes
  proof: `NewContentSearchReadAdmissionWithReadStore(db.ReadStore, …)`
  serves all ten reads over nothing wider than the guarded `ReadStore`
  (fail-closed on an unavailable reader), the full wire-object byte
  mirror against `*ContentReader` over the same store (paged, fallback,
  corpus-wide, install, and PHP shapes, plus found/empty edges and
  unscoped budget-validation parity), and the constructor budget proof
  (`TestContentSearchReadAdmissionConstructorReproducesProductionBudget`
  — dynamic type `*ContentReader` with the passed budget, logger, and
  instruments applied).
- `content_search_read_admission_routes_test.go` — shared-double
  handler tests for all four routes through the real mux
  (paged-seam, bounded-walk, single-repo-fallback, and corpus-wide
  branches with exact recorded-call assertions, plus the installer and
  PHP pages), and the route/spec-fragment parity test (every pinned
  path in the OpenAPI content fragment and served by `Mount`).
- Compile-time mirror assertion in the pin file:
  `var _ ContentSearchReadAdmission = (*ContentReader)(nil)`.

## Census (per route per method)

| Method | `files/search` | `entities/search` | `dockerfile-installs/search` | `php-config/search` |
|---|---|---|---|---|
| `SearchFiles` | paged seam (scoped) | — | — | — |
| `SearchFileContent` | single-repo fallback | — | — | — |
| `SearchFileContentAnyRepo` | corpus fallback (unscoped, no seams) | — | — | — |
| `SearchFilesUnscoped` | bounded walk (unscoped) | — | — | — |
| `SearchEntities` | — | paged seam (scoped) | — | — |
| `SearchEntityContent` | — | single-repo fallback | — | — |
| `SearchEntityContentAnyRepo` | — | corpus fallback | — | — |
| `SearchEntitiesUnscoped` | — | bounded walk (unscoped) | — | — |
| `SearchDockerfileInstallEntities` | — | — | install-entity page | — |
| `SearchPHPConfigEntities` | — | — | — | PHP evidence page |

Every IN method above is exercised by at least one route subtest that
records the exact call through the real mux. Seams take precedence by
handler construction (`PagedContentSearcher`, then fallback;
`Unscoped*Searcher` for the unscoped shape); the fallback subtests use
a seam-free double so the recorded branch is the fallback one.

## Explicitly OUT (not widened by this pin)

- Point reads (`GetFileContent`, `GetFileLines`, `GetEntityContent`
  and the scoped/entity-list twins) — the D13 point pin covers them.
- Name-search twins (`SearchEntitiesByName`,
  `SearchEntitiesByNameAnyRepo`), exact-name twins, the entity-name
  index (`SearchEntityNames`), the symbol-aware read
  (`SearchSymbols`), the candidate pages (`SearchCodeCandidates`),
  and the by-ID hydration (`ListRepoEntitiesByIDs`) — the Code search
  pin covers them.
- `SearchFileContentAnyRepoExactCase` — exact-case token scans; no
  content search route reaches it.
- `SearchFileReferenceAnyRepo` — normalized reference indexes; no
  content search route reaches it.
- `SearchEntitiesByLanguageAndType` / `...ForAccess`,
  `SearchEntitiesReferencingComponent`,
  `SearchEntitiesByNameInRepositories`, `GetEntityContents` /
  `GetEntityContentsInRepositories`, `GetFileContents`,
  `NonBlankRepoFilePaths`, and every other `List*` leg — other
  families' standing, unchanged.
- Grant checks, selector resolution, fixed failure messages, and
  `QueryProfile` (handler behavior, not port shape).
- The semantic/hybrid rerank (`HybridRanker`, gated on the embedder;
  lexical order otherwise) — handler behavior, not port shape.
  SemanticSearch v0 is a record-only cross-reference: no semantic
  method enters this pin and nothing semantic is touched.
- Ledger / stored-summary / snapshot surface (none on this family;
  sealed out regardless).

## Satellite consumption

Build the satisfier with
`query.NewContentSearchReadAdmissionWithReadStore(reader, budget,
logger, instruments)` where `reader` is the request-scoped
`db.ReadStore` and `(budget, logger, instruments)` are the
unscoped-search budget, logger, and instruments the wiring resolves
once at startup (`unscoped.BudgetFromEnv` for the budget).
