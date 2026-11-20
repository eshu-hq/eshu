# EntityReadAdmission v0 (entities pin record)

Refs eshu-hq/eshu#9669. Wave-5 Entities tier-b core pin, per the arbiter step-2 scope ruling (decisions 2, 6, 7). Minted core-side; the satellite records this pin in a follow-up. A satellite PR consuming an unminted pin is invalid. Arbiter review of this pin precedes satellite termination of the Entities legs.

## Shape

Version: `EntityReadAdmissionContractVersion = "v0"` (`go/internal/query/entity/read_admission.go`).

Exactly 30 re-declared (never embedded) methods, sealed — no write surface, no builder method, no grant-check shape rides along:

| # | Method | Mirrors | Serves |
|---|--------|---------|--------|
| 1 | `Run`, `RunSingle` | `querycontract.GraphQuery` | bounded graph reads: direct, selector-catalog fallback, workload-candidate resolution |
| 2 | `GetEntityContent`, `SearchEntitiesByName`, `SearchEntitiesByNameAnyRepo`, `SearchEntitiesReferencingComponent`, `SearchFileContentAnyRepo`, `SearchFileContentAnyRepoExactCase`, `ResolveRepository`, `ListRepoEntities`, `ListRepoEntitiesByKeys`, `ListRepoEntitiesByIDs`, `ListRepoEntitiesByType`, `ListRepoEntitiesByTypes`, `MatchRepositories`, `ListRepositories`, `ListRepoFiles`, `GetFileContent`, `GetFileContents`, `NonBlankRepoFilePaths`, `ListFrameworkRoutes` | `querycontract.ContentStore` | the 19 family-closure content reads |
| 3 | `SearchEntityNames` | `EntityNameSearcher` facet | global resolve path |
| 4 | `SearchFileReferenceAnyRepo` | content-reference search facet | consumer-evidence indexed scan |
| 5 | `ListCICDRunCorrelations` | `querycontract.CICDRunCorrelationStore` | workload-story CI/CD evidence |
| 6 | `ListContainerImageIdentities`, `ListSBOMAttestationAttachments` | supply-chain stores | service-story supply-chain reads |
| 7 | `ServiceStoryTargetSupportEvidence`, `DocumentationFindings`, `RepositoryDeploymentEvidence`, `RepositoryReadModelSummary` | narrow read-model facets | story, deployment-evidence, and read-model identity stages |

Route oracle codes (oracle `8a78eb3f48`): `POST /api/v0/entities/resolve` (`paths/search/entities.go`: 200/400/403/500/503/504), `GET /api/v0/entities/{entity_id}/context` (`paths/search/entities.go`: 200/400/403/404/500/503/504), `GET /api/v0/workloads/{workload_id}/context` (`paths/search/entities.go`: 200/400/403/404/500/503/504), `GET /api/v0/workloads/{workload_id}/story` (`paths/search/entities.go`: 200/400/403/404/500/503/504), `GET /api/v0/services/{service_name}/context` (`paths/search/entities.go`: 200/400/403/404/409/500/503/504), `GET /api/v0/services/{service_name}/story` (`paths/search/entities.go`: 200/400/403/404/409/500/503/504), `GET /api/v0/services/{service_name}/api-surface/endpoints` (`paths/search/entities.go`: 200/400/403/404/409/500/503/504), `GET /api/v0/services/{service_name}/downstream-consumers` (`paths/search/entities.go`: 200/400/403/404/409/500/503/504), `GET /api/v0/investigations/services/{service_name}` (`paths/evidence/investigations.go`: 200/400/403/404/500/503/504).

Transport: no `WithReadStore` constructor on this pin — the Entities seam spans the graph reader, the content reader, the CI/CD correlation store, the two supply-chain stores, and the four narrow read-model facets, and minting one constructor would pick a wiring the satellite never runs. The compile-time mirror assertion in the pin file plus the production mirror (`go/internal/query/w5_entity_read_admission_mirror.go`: `*Neo4jReader` + `*ContentReader` + `PostgresCICDRunCorrelationStore` + the two supply-chain stores) prove the stores the legs run against today satisfy the pin. Concrete-double-only proof is rejected.

Enforcement (all in `go/internal/query/entity/`):

- `read_admission_contract_test.go` — frozen method-set test plus version pin (`TestEntityReadAdmissionContractIsVersioned`), witness (`entityReadAdmissionWitness` + satisfier assertion) and its re-signed twin (`TestEntityReadAdmissionRejectsResignedMethod`), seal (`TestEntityReadAdmissionWireShapesAreSealed` — exactly the 30-method frozen set, no write/builder/grant surface), and the seeded-drift RED half (`TestEntityReadAdmissionSeededDriftIsDetected` — a planted widening must not match the frozen set).
- `read_admission_wire_shapes_test.go` — satellite-consumability (`TestEntityReadAdmissionWireShapesAreSatelliteConsumable`).

## Satellite consumption

Depend on the shape, not a constructor: serve the nine legs through the production-mirror composition (graph + content + CI/CD correlation + supply-chain + read-model facets), the same stores the legs run against today. A satellite PR consuming this pin for any other backend combination is invalid without arbiter review. The W5c-e impact deployment-trace enrichment legs cite this pin alongside `ImpactReadAdmission` for their TraceContext leg (dual-pin cite recorded in both pins); silent reliance on the Entity pin is forbidden.

## W8a disclosed-absence posture

All nine routes are natively mounted in-tree and their OpenAPI fragments stay core-side (oracle `paths/search/entities.go` and `paths/evidence/investigations.go`, codes above) — disclosed here, not silently dropped. No `contracts/upstream/*` stub covers these legs: the wire-shape generator snapshots forwarded relay spellings only, and no Entities leg has one.

## Explicitly OUT — `GET /api/v0/graph/entities` rides GraphEntityInventoryReadAdmission, never this pin

`GET /api/v0/graph/entities` is W8b-owned satellite-side and keeps its own standing under `GraphEntityInventoryReadAdmission` (`go/internal/query/entities_read_admission.go`); the 9th admitted slot belongs to the investigation route in the census above. A satellite PR citing this record as graph-entities coverage is invalid. The impact `trace-deployment-chain` and `deployment-config-influence` legs cite this pin alongside `ImpactReadAdmission` for their TraceContext leg only — that cite admits no other impact route here.

Also OUT (each keeps its own standing until a deliberate pin covers it):

- The content-fallback relationship builder (`ContentRelationshipBuilder.BuildContentRelationships`): the METHOD stays out — the production satisfier is the satellite-importable `entity/content/fallback` Builder — while its two exercised reads (`SearchEntitiesReferencingComponent`, `ListRepoEntitiesByType`) are frozen here.
- The optional image-candidate explainer facet (`ExplainContainerImageCandidate`), the `RepositoryContextCoverage` / `RepositoryFilesLastIndexedAt` facets, `GraphBackend`, profile, logger, instruments, and the fixed failure messages (handler behavior, not port shape).
- The grant checks (scoped grant comparisons, scoped/empty-grant parity, proven per route at byte-parity time, never frozen here).
- No suppression leg exists on these routes; the Decision-2 suppression-state-filtering parity obligation stays on the supply-chain legs this pin does not touch.

## Rollback

Revert this pin. The seam is unwired — no handler, route, or caller depends on it yet — so rollback moves no traffic and changes no data.
