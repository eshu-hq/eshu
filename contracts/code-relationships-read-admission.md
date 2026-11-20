# CodeRelationshipsReadAdmission v0 (core pin record)

Refs eshu-hq/eshu#9669. Wave-4 Code tier-b core pin, per the arbiter
step-2 scope ruling (decisions 2, 6, 7). Minted core-side; the satellite
records this pin in a follow-up. A satellite PR consuming an unminted
pin is invalid. Arbiter review of this pin precedes satellite
termination of the G-b relationship legs.

## Shape

Version: `CodeRelationshipReadAdmissionContractVersion = "v0"`
(go/internal/query/codequery/code_relationships_read_admission.go —
the pin type is singular `CodeRelationshipReadAdmission`; the record
keeps the route-family plural).

Exactly 17 re-declared (never embedded) methods, sealed — no ledger,
stored-summary, snapshot, or write surface rides along:

| # | Method | Mirrors | Serves |
|---|--------|---------|--------|
| 1 | `Run` | `querycontract.GraphQuery` | `POST /api/v0/code/relationships`, `POST /api/v0/code/relationships/story` bounded traversals |
| 2 | `RunSingle` | `querycontract.GraphQuery` | single-row graph reads on both legs |
| 3 | `GetEntityContent` | staying `ContentStore` port | story evidence hydration |
| 4 | `SearchEntitiesByName` | staying `ContentStore` port | both legs, name resolution |
| 5 | `SearchEntitiesByNameAnyRepo` | staying `ContentStore` port | corpus-wide name fallback |
| 6 | `SearchEntitiesByExactName` | staying `ContentStore` port | exact-name resolution |
| 7 | `SearchEntitiesByExactNameAnyRepo` | staying `ContentStore` port | corpus-wide exact-name fallback |
| 8 | `SearchEntitiesByLanguageAndType` | staying `ContentStore` port | language/type reads |
| 9 | `SearchEntitiesReferencingComponent` | staying `ContentStore` port | component-reference reads |
| 10 | `ListRepoFiles` | staying `ContentStore` port | repository file lists |
| 11 | `ListRepoEntitiesByType` | staying `ContentStore` port | typed entity lists |
| 12 | `ListRepoEntitiesByIDs` | staying `ContentStore` port | by-ID hydration |
| 13 | `ListRepositories` | staying catalog port | repository catalog |
| 14 | `MatchRepositories` | staying catalog port | repository matching |
| 15 | `GetEntityContentInRepositories` | staying `ContentStore` port | scoped-entity hydration |
| 16 | `SearchEntitiesByNameInRepositories` | staying `ContentStore` port | in-repository name reads |
| 17 | `SearchEntitiesByLanguageAndTypeForAccess` | staying grant-bound port | grant-bound language/type reads |

Route oracle codes (oracle `8a78eb3f48`,
`paths/code/routes.go`): `POST /api/v0/code/relationships`
(200/400/403/404/500/503/504), `POST
/api/v0/code/relationships/story` (200/400/403/500/503/504).

Transport: no `WithReadStore` constructor on this pin — the G-b seam
spans the graph reader, twelve content-store reads, the catalog pair,
and the grant-bound upgrade, and minting one constructor would pick a
wiring the satellite never runs. The compile-time mirror assertion in
the pin file proves the tuple the legs run against today satisfies the
pin.

Evidence (not enforcement): the wire-shapes proof through the real
`Mount` (`code_relationships_read_admission_wire_shapes_test.go` —
`TestCodeRelationshipReadAdmissionWireShapesAreSatelliteConsumable`).

Enforcement (all in go/internal/query/codequery):

- `code_relationships_read_admission_contract_test.go` — frozen
  method-set test plus version pin
  (`TestCodeRelationshipReadAdmissionContractIsVersioned`), witness
  (`codeRelationshipReadAdmissionWitness` + satisfier assertion), its
  re-signed twin
  (`TestCodeRelationshipReadAdmissionRejectsResignedMethod`), the
  widening guard
  (`TestCodeRelationshipReadAdmissionWideningIsCaughtByFrozenSet` —
  a 17+1 widened witness still satisfies the pin by assignability, so
  only the frozen list rejects widening), seal
  (`TestCodeRelationshipReadAdmissionWireShapesAreSealed` — exactly
  17 methods, no ledger/summary/snapshot/append/exec/write surface),
  and the seeded-drift RED half
  (`TestCodeRelationshipReadAdmissionSeededDriftIsDetected` — the
  planted `InspectStructuralInventory` widening must not match the
  frozen set).

## Satellite consumption

Depend on the shape, not a constructor: serve both legs through the
existing graph reader, content store, catalog, and grant-bound upgrade,
the same tuple the legs run against today. A satellite PR consuming
this pin for any other backend tuple is invalid without arbiter review.

## W8a disclosed-absence posture

`POST /api/v0/code/relationships` and `POST
/api/v0/code/relationships/story` are natively mounted in-tree
(`CodeHandler.Mount`, go/internal/query/codequery/handler.go) and
their OpenAPI fragments stay core-side (oracle `paths/code/routes.go`,
codes above) — disclosed here, not silently dropped. No
`contracts/upstream/*` stub covers these legs: the wire-shape generator
snapshots forwarded relay spellings only, and no G-b leg has one.

## Explicitly OUT (not widened by this pin)

- `POST /api/v0/code/call-graph/metrics` — codeGraphWave-owned; this
  Wave-4 record must never absorb it (see
  contracts/code-graph-read-admission.md).
- Structural-inventory and topic projections — the content
  investigation pin covers them (see
  contracts/code-content-investigation-read-admission.md).
- Dockerfile-install and PHP-config entity pages — the Content search
  pin covers them.
- `SearchFileContentAnyRepoExactCase`, `SearchFileReferenceAnyRepo` —
  no relationships leg reaches them.
- Grant checks, the story-clause builder, fixed failure messages, and
  `QueryProfile` (handler behavior, not port shape).

## Rollback

Revert this pin. The seam is unwired — no handler, route, or caller
depends on it yet — so rollback moves no traffic and changes no data.
