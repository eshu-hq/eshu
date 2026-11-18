# CodeLanguageQueryReadAdmission v0 (core pin record)

Refs eshu-hq/eshu#9669. Wave-4 Language tier-b core pin, per the
arbiter step-2 scope ruling (decisions 2, 6, 7). Minted core-side; the
satellite records this pin in a follow-up. A satellite PR consuming an
unminted pin is invalid. Arbiter review of this pin precedes satellite
termination of the language-query leg.

## Shape

Version: `CodeLanguageQueryReadAdmissionContractVersion = "v0"`
(go/internal/query/language/code_query_read_admission.go — the pin
lives in package language, not codequery: `language.Handler` mounts
the route and codequery cannot mount it without an import cycle. Do not
move the handler).

Exactly 4 re-declared (never embedded) methods, sealed — `RunSingle`
is OUT by construction, and no name, exact-name, component, file,
catalog, or story surface rides along:

| # | Method | Mirrors | Serves |
|---|--------|---------|--------|
| 1 | `Run` | `querycontract.GraphQuery` | `POST /api/v0/code/language-query` graph-backed, graph-first-content, and directory branches |
| 2 | `SearchEntitiesByLanguageAndType` | staying `ContentStore` port | content-only branch unscoped lookup |
| 3 | `SearchEntitiesByLanguageAndTypeForAccess` | staying grant-bound port | grant-bound enrichment lookup |
| 4 | `ListEntityMetadataByIDsForAccess` | staying grant-bound port | id-anchored metadata enrichment |

Route oracle codes (oracle `8a78eb3f48`,
`paths/code/routes.go`): `POST /api/v0/code/language-query`
(200/400/403/500/501/503/504).

Transport: no `WithReadStore` constructor on this pin — the
language-query seam spans the graph reader plus three content reads
(including the two optional-interface upgrades the enrichment
type-asserts) and minting one constructor would pick a wiring the
satellite never runs. The compile-time mirror assertion in the pin file
proves the tuple the route runs against today satisfies the pin.

Evidence (not enforcement): the wire-shapes proof through the real
language mux
(`code_query_read_admission_wire_shapes_test.go` —
`TestCodeLanguageQueryReadAdmissionWireShapesAreSatelliteConsumable`).

Enforcement (all in go/internal/query/language):

- `code_query_read_admission_contract_test.go` — frozen method-set
  test plus version pin
  (`TestCodeLanguageQueryReadAdmissionContractIsVersioned`), witness
  (`codeLanguageQueryReadAdmissionWitness` + satisfier assertion) and
  its re-signed twin
  (`TestCodeLanguageQueryReadAdmissionRejectsResignedMethod` — a
  re-signed `SearchEntitiesByLanguageAndTypeForAccess` must break the
  pin), seal
  (`TestCodeLanguageQueryReadAdmissionWireShapesAreSealed` — exactly
  4 methods, no `RunSingle`/name/exact-name/component/file/catalog
  surface), and the seeded-drift RED half
  (`TestCodeLanguageQueryReadAdmissionSeededDriftIsDetected` — the
  planted `RunSingle` widening must not match the frozen set).

## Satellite consumption

Depend on the shape, not a constructor: serve the route through the
existing graph reader plus the guarded Postgres content reader with the
same optional-interface upgrades the enrichment type-asserts today. A
satellite PR consuming this pin for any other backend tuple is invalid
without arbiter review.

## W8a disclosed-absence posture

`POST /api/v0/code/language-query` is natively mounted in-tree
(`language.Handler`, go/internal/query/language/handler.go) and its
OpenAPI fragment stays core-side (oracle `paths/code/routes.go`,
200/400/403/500/501/503/504) — disclosed here, not silently dropped. No
`contracts/upstream/*` stub covers this leg: the wire-shape generator
snapshots forwarded relay spellings only, and no language leg has one.

## Explicitly OUT (not widened by this pin)

- `RunSingle` — the single-row graph read; the language-query lane
  never issues one. Proven at the seal, not just in the frozen set.
- `SearchEntitiesByName` / `SearchEntitiesByNameAnyRepo` (global
  index), `SearchSymbols`, `SearchCodeCandidates`, and the by-ID
  hydration — the Code search pin covers them.
- Per-repo fallback loops and the page-read fallback behind the two
  optional interfaces — handler behavior, not port shape; the
  satellite keeps them, this pin carries the read shape only.
- Grant checks, the repository selector, fixed failure messages, and
  `QueryProfile` (handler behavior, not port shape).

## Rollback

Revert this pin. The seam is unwired — no handler, route, or caller
depends on it yet — so rollback moves no traffic and changes no data.
