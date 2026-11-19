# RepositoryStoryReadAdmission & RepositoryReadsReadAdmission v0 (repositories pin record)

Refs eshu-hq/eshu#9669. Wave-5 Repositories tier-b core pin, per the arbiter step-2 scope ruling (decisions 2, 6, 7). Minted core-side; the satellite records this pin in a follow-up. A satellite PR consuming an unminted pin is invalid. Arbiter review of this pin precedes satellite termination of the Repositories legs.

## Shape

Versions: `RepositoryStoryReadAdmissionContractVersion = "v0"` (`go/internal/query/repository/read_admission.go`, 15 methods, story route ONLY — the D4 carve-out: the D4 transport record covers the 11 terminated repository GETs and explicitly excludes story, so retroactive widening is forbidden) and `RepositoryReadsReadAdmissionContractVersion = "v0"` (`go/internal/query/repository/reads_read_admission.go`, 17 methods, the other 11 routes). Thirty-two re-declared (never embedded) methods total, sealed:

| # | Method | Mirrors | Serves |
|---|--------|---------|--------|
| 1 | story `Run`, story `RunSingle` | `querycontract.GraphQuery` | repository base row, story/dependency/deployment-evidence/artifact traversals, selector fallback |
| 2 | story `MatchRepositories`, `RepositoryCoverage`, `ListRepoEntities`, `ListRepoFiles`, `ListRepoEntitiesByTypes`, `GetFileContent`, `GetFileContents`, `NonBlankRepoFilePaths` | `querycontract.ContentStore` | path selector, coverage, semantic, hydration, and config stages |
| 3 | story `ListCICDRunCorrelations` | CI/CD correlation store | ci/cd evidence stage |
| 4 | story `RepositoryReadModelSummary`, `ServiceStoryTargetSupportEvidence`, `DocumentationFindings`, `RepositoryDeploymentEvidence` | narrow read-model facets | graph summary, target support, documentation, deployment-evidence stages |
| 5 | reads `Run`, reads `RunSingle` | `querycontract.GraphQuery` | list, context, and stats legs |
| 6 | reads `GetFileContent`, `ListRepoFiles`, `ListRepoFilesByLanguage`, `ListRepoFilesByPath`, `RepoFilePathContext`, `RepositoryCoverage`, `ResolveRepository`, `ReadRepositoriesByLanguage`, `CountRepositoriesByLanguage`, `RepositoryLanguageInventory` | content + tree/catalog narrow facets | content, tree, context, branches, stats, by-language, and language-inventory legs |
| 7 | reads `ListWorkloadIdentities`, `ListServiceCatalogCorrelations` | catalog facets | catalog leg |
| 8 | reads `ReadRepositoryFreshness` | freshness reader | freshness leg |
| 9 | reads `ReadPageSelectionsBySlug`, `ReadPageRetirements` | selection/retirement readers | list annotations |

Route oracle codes (oracle `8a78eb3f48`): `GET /api/v0/catalog` (`paths/repository/routes.go`: 200/403/500/503/504), `GET /api/v0/repositories` (`paths/repository/routes.go`: 200/400/403/500/503/504), `GET /api/v0/repositories/by-language` (`paths/repository/routes.go`: 200/400/403/500/503), `GET /api/v0/repositories/language-inventory` (`paths/repository/routes.go`: 200/403/500/503), `GET /api/v0/repositories/{repo_id}/branches` (`paths/repository/branches.go`: 200/400/404/500/503/504), `GET /api/v0/repositories/{repo_id}/content` (`paths/repository/routes.go`: 200/400/404/409/500/503/504), `GET /api/v0/repositories/{repo_id}/context` (`paths/repository/routes.go`: 200/400/403/404/500/503/504), `GET /api/v0/repositories/{repo_id}/story` (`paths/repository/routes.go`: 200/400/403/404/500/503/504), `GET /api/v0/repositories/{repo_id}/tree` (`paths/repository/routes.go`: 200/400/403/404/409/500/503/504), `GET /api/v0/repositories/{repo_id}/coverage` (`paths/repository/stats_and_coverage.go`: 200/400/403/404/500/503/504), `GET /api/v0/repositories/{repo_id}/stats` (`paths/repository/stats_and_coverage.go`: 200/400/403/404/500/503/504), `GET /api/v0/repositories/{repo_id}/freshness` (`paths/repository/freshness.go`: 200/400/403/404/500/503/504).

Transport: no `WithReadStore` constructor on these pins — minting one would pick a wiring the satellite never runs. The compile-time mirror assertions in each pin file prove the stores the legs run against today satisfy the pins. Concrete-double-only proof is rejected.

Enforcement (all in `go/internal/query/repository/`):

- `read_admission_contract_test.go` (story) — version pin (`TestRepositoryStoryReadAdmissionContractIsVersioned`), witness (`repositoryStoryReadAdmissionWitness` + satisfier assertion) and its re-signed twin (`TestRepositoryStoryReadAdmissionRejectsResignedMethod`), seal (`TestRepositoryStoryReadAdmissionWireShapesAreSealed` — exactly the 15-method frozen set), and the seeded-drift RED half (`TestRepositoryStoryReadAdmissionSeededDriftIsDetected`).
- `reads_read_admission_contract_test.go` (reads) — version pin (`TestRepositoryReadsReadAdmissionContractIsVersioned`), witness (`repositoryReadsReadAdmissionWitness` + satisfier assertion), seal (`TestRepositoryReadsReadAdmissionWireShapesAreSealed` — exactly the 17-method frozen set), and the seeded-drift RED half (`TestRepositoryReadsReadAdmissionSeededDriftIsDetected`).
- `read_admission_wire_shapes_test.go` — satellite-consumability (`TestRepositoryStoryReadAdmissionWireShapesAreSatelliteConsumable`).

## Satellite consumption

Depend on the shape, not a constructor: serve the story leg through the story-pin composition and the other eleven legs through the reads-pin composition, the same stores the legs run against today. No route may cite the sibling pin — the impact packet single rides its own packet pin, and a satellite PR consuming either pin for any other backend combination is invalid without arbiter review.

## W8a disclosed-absence posture

All twelve routes are natively mounted in-tree and their OpenAPI fragments stay core-side (oracle `paths/repository/routes.go`, `branches.go`, `stats_and_coverage.go`, and `freshness.go`, codes above) — disclosed here, not silently dropped. No `contracts/upstream/*` stub covers these legs: the wire-shape generator snapshots forwarded relay spellings only, and no Repositories leg has one.

## Explicitly OUT — story rides its sibling pin, never a whole-Repositories pin

The repository story read (`GET /api/v0/repositories/{repo_id}/story`) rides `RepositoryStoryReadAdmission` only: minting a whole-Repositories story here would retroactively widen D4, which stays forbidden. Also OUT (each keeps its own standing until a deliberate pin covers it):

- `ResolveRepository` and `ListRepositories` on the story pin (the story resolves by `RunSingle` plus `MatchRepositories`; those fallbacks belong to the stats/tree/freshness/branches/list legs) and `RepositoryContextCoverage` (the story coverage stage calls `RepositoryCoverage` directly).
- `GetEntityContent`, the `Search*` family beyond `MatchRepositories`, `ListRepoEntitiesByKeys/ByIDs/ByPaths`, `ListRepoK8sSelectCandidates`, `ListFrameworkRoutes`, language aggregates, `MatchRepositoriesByIDs`, `RepositoryFilesLastIndexedAt`, `GetFileLines` (no story stage calls them).
- Freshness, selections, retirements, and service-catalog correlation reads on the story pin (list and catalog legs, never story).
- The manifest-dependency section of the context response (rides the `ManifestDependencies` pin; a nil store omits the section).
- The CI/CD run-correlation read on the reads pin (rides the sibling CICD pin the story pin already cites).
- Loader internals, responder/writer seams, fixed failure messages, and the Decision-2 suppression parity obligation (handler behavior or other lanes, never frozen here).

## Rollback

Revert this pin. The seam is unwired — no handler, route, or caller depends on it yet — so rollback moves no traffic and changes no data.
