# CodeDeadCodeReadAdmission v0 (core pin record)

Refs eshu-hq/eshu#9669. Wave-4 Code tier-b core pin, per the arbiter
step-2 scope ruling (decisions 2, 6, 7). Minted core-side; the satellite
records this pin in a follow-up. A satellite PR consuming an unminted
pin is invalid. Arbiter review of this pin precedes satellite
termination of the dead-code legs.

## Shape

Version: `CodeDeadCodeReadAdmissionContractVersion = "v0"`
(go/internal/query/code_dead_code_read_admission.go).

Exactly 14 re-declared (never embedded) methods, sealed — `RunSingle`
is OUT by construction, and no search, list (beyond the pinned
hydration pair), file, ledger, stored-summary, or snapshot surface rides
along:

| # | Method | Mirrors | Serves |
|---|--------|---------|--------|
| 1 | `Run` | `querycontract.GraphQuery` | SQL-routine reachability probes behind all three legs |
| 2 | `DeadCodeCandidateRows` | `codeshaping.DeadCodeCandidateQuery` | `POST /api/v0/code/dead-code`, `POST /api/v0/code/dead-code/investigate` candidate reads |
| 3 | `DeadCodeIncomingEntityIDs` | staying handler port | investigation + scan incoming-edge reads |
| 4 | `CodeReachabilityIncomingEntityIDs` | staying handler port | investigation + scan reachability reads |
| 5 | `DowngradedCodeRootKinds` | staying handler port | investigation + scan downgrade reads |
| 6 | `GetEntityContents` | `*ContentReader` | candidate hydration (multi-get) |
| 7 | `GetEntityContent` | `*ContentReader` | candidate hydration (single) |
| 8 | `RepositoryContextCoverage` | staying handler port | coverage-block freshness reads |
| 9 | `RepositoryFilesLastIndexedAt` | staying handler port | coverage-block freshness reads |
| 10 | `RepositoryCoverage` | staying handler port | coverage-block full aggregate |
| 11 | `CrossRepoDeadCodeConsumerEvidence` | staying handler port | `POST /api/v0/code/dead-code/cross-repo` consumer evidence |
| 12 | `CrossRepoDeadCodeConsumerCoverage` | staying handler port | cross-repo consumer coverage |
| 13 | `CrossRepoDeadCodeConsumerRootPaths` | staying handler port | cross-repo consumer root paths |
| 14 | `RepositoryRelationshipReadModel` | staying handler port | cross-repo boundary evidence |

Route oracle codes (oracle `8a78eb3f48`): `POST
/api/v0/code/dead-code` (`paths/code/dead/scan.go`:
200/400/403/500/503/504), `POST /api/v0/code/dead-code/investigate`
(`paths/code/dead/investigation.go`: 200/400/403/500/503/504), `POST
/api/v0/code/dead-code/cross-repo` (`paths/code/dead/cross_repo.go`:
200/400/403/500/503/504).

Transport: no `WithReadStore` constructor on this pin — the dead-code
seam spans the graph run plus thirteen content-scoped reads and minting
one constructor would pick a wiring the satellite never runs. The
compile-time mirror assertion in the pin file
(`var _ CodeDeadCodeReadAdmission =
codeDeadCodeReadAdmissionWitness{}`) proves the pair the legs run
against today satisfies the pin. Concrete-double-only proof is rejected.

Enforcement (all in go/internal/query):

- `code_dead_code_read_admission_contract_test.go` — frozen method-set
  test plus version pin
  (`TestCodeDeadCodeReadAdmissionContractIsVersioned`), witness
  (`codeDeadCodeReadAdmissionContractWitness` + satisfier assertion),
  seal (`TestCodeDeadCodeReadAdmissionWireShapesAreSealed` — exactly
  14 methods, no `RunSingle`/search/list/file/ledger/summary/snapshot
  surface), and the seeded-drift RED half
  (`TestCodeDeadCodeReadAdmissionSeededDriftIsDetected` — the planted
  `RunSingle` widening must not match the frozen set).

## Satellite consumption

Depend on the shape, not a constructor: serve the three legs' reads
through the existing graph reader plus the guarded Postgres content
reader, the same pair the legs run against today. A satellite PR
consuming this pin for any other backend pair is invalid without
arbiter review.

## W8a disclosed-absence posture

`POST /api/v0/code/dead-code`, `POST
/api/v0/code/dead-code/investigate`, and `POST
/api/v0/code/dead-code/cross-repo` are natively mounted in-tree
(`CodeHandler.Mount`, go/internal/query/codequery/handler.go) and
their OpenAPI fragments stay core-side (oracle `paths/code/dead/`,
codes above) — disclosed here, not silently dropped. No
`contracts/upstream/*` stub covers these legs: the wire-shape generator
snapshots forwarded relay spellings only, and no dead-code leg has one.

## Explicitly OUT (not widened by this pin)

- `RunSingle` — the single-row graph read; the dead-code lane never
  issues one. Proven at the seal, not just in the frozen set.
- Name, exact-name, component-reference, and language/type searches —
  the Code search and relationships pins cover them.
- File-content and file-lines point reads — the D13 point pin covers
  them.
- Grant checks, repository selector, fixed failure messages, and
  `QueryProfile` (handler behavior, not port shape).
- Ledger / stored-summary / snapshot surface (none on this family;
  sealed out regardless).

## Rollback

Revert this pin. The seam is unwired — no handler, route, or caller
depends on it yet — so rollback moves no traffic and changes no data.
