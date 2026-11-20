# ImpactReadAdmission v0 (impact pin record)

Refs eshu-hq/eshu#9669. Wave-5 Impact tier-b core pin, per the arbiter step-2 scope ruling (decisions 2, 6, 7). Minted core-side; the satellite records this pin in a follow-up. A satellite PR consuming an unminted pin is invalid. Arbiter review of this pin precedes satellite termination of the Impact legs.

## Shape

Version: `ImpactReadAdmissionContractVersion = "v0"` (`go/internal/query/impact/read_admission.go`).

Exactly 14 re-declared (never embedded) methods, sealed — no write surface, no prober seam, no catalog shape rides along:

| # | Method | Mirrors | Serves |
|---|--------|---------|--------|
| 1 | `Run`, `RunSingle` | `querycontract.GraphQuery` | bounded graph reads: direct, selector/workload resolution, path-decoder passed reader |
| 2 | `GetEntityContent`, `SearchEntitiesByName`, `SearchEntitiesByExactNameAnyRepo`, `ListRepoEntities`, `ListRepoEntitiesByType`, `ListRepoEntitiesByPaths`, `ListRepoEntitiesByIDs`, `ListRepoK8sSelectCandidates` | `querycontract.ContentStore` | anchor hydration, deployment-trace, change-surface, and SELECTS scans |
| 3 | `InvestigateCodeTopic` | code-topic investigator | change-surface root adapter |
| 4 | `HasLiveIdentityMatch`, `ListLiveIdentityMatches` | pod-template store | live pod-template reads |
| 5 | `ListRunningTasks` | ECS task store | running-task read |

Route oracle codes (oracle `8a78eb3f48`): `POST /api/v0/impact/blast-radius` (`paths/impact/routes.go`: 200/400/403/500/503/504), `POST /api/v0/impact/change-surface` (`paths/impact/routes.go`: 200/400/403/500/503/504), `POST /api/v0/impact/change-surface/investigate` (`paths/impact/routes.go`: 200/400/403/500/501/503/504), `POST /api/v0/impact/contracts` (`paths/impact/contract.go`: 200/400/403/500/501/503/504), `POST /api/v0/impact/deployment-config-influence` (`paths/impact/deployment_config_influence.go`: 200/400/403/404/409/500/503/504), `POST /api/v0/impact/developer-change-plan` (`paths/impact/rest.go`: 200/400/403/500/501/503/504), `POST /api/v0/impact/entity-map` (`paths/impact/rest.go`: 200/400/500/501/503/504), `POST /api/v0/impact/explain-dependency-path` (`paths/impact/rest.go`: 200/400/403/404/500/503/504), `POST /api/v0/impact/pre-change` (`paths/impact/rest.go`: 200/400/403/500/501/503/504), `POST /api/v0/impact/resource-investigation` (`paths/impact/rest.go`: 200/400/403/500/501/503/504), `POST /api/v0/impact/trace-resource-to-code` (`paths/impact/rest.go`: 200/400/403/500/503/504), `POST /api/v0/impact/trace-deployment-chain` (`paths/impact/routes.go`: 200/400/403/404/409/500/503/504), `POST /api/v0/impact/trace-exposure-path` (`paths/impact/exposure.go`: 200/400/403/500/501/503/504).

Transport: no `WithReadStore` constructor on this pin — the Impact seam spans the graph reader, the content reader, the code-topic investigator, and the live/ECS stores, and minting one constructor would pick a wiring the satellite never runs. The compile-time production mirror (`go/internal/query/w5_impact_read_admission_mirror.go`: `*Neo4jReader` + `*ContentReader` + `PostgresKubernetesPodTemplateStore` + `PostgresECSTaskStore`) proves the stores the legs run against today satisfy the pin. Concrete-double-only proof is rejected.

Enforcement (all in `go/internal/query/impact/`):

- `read_admission_contract_test.go` — frozen method-set test plus version pin (`TestImpactReadAdmissionContractIsVersioned`), witness (`impactReadAdmissionWitness` + satisfier assertion) and its re-signed twin (`TestImpactReadAdmissionRejectsResignedMethod`), seal (`TestImpactReadAdmissionWireShapesAreSealed` — exactly the 14-method frozen set), and the seeded-drift RED half (`TestImpactReadAdmissionSeededDriftIsDetected` — a planted widening must not match the frozen set).
- `read_admission_wire_shapes_test.go` — satellite-consumability (`TestImpactReadAdmissionWireShapesAreSatelliteConsumable`).

## Satellite consumption

Depend on the shape, not a constructor: serve the thirteen legs through the production-mirror composition, the same stores the legs run against today. A satellite PR consuming this pin for any other backend combination is invalid without arbiter review. The deployment-trace enrichment legs (`trace-deployment-chain`, `deployment-config-influence`) additionally require `EntityReadAdmission` for their TraceContext leg (explicit dual-pin cite recorded here and in the Entity pin); silent reliance on the Entity pin is forbidden.

## W8a disclosed-absence posture

All thirteen POSTs are natively mounted in-tree and their OpenAPI fragments stay core-side (oracle `paths/impact/routes.go`, `contract.go`, `deployment_config_influence.go`, `rest.go`, and `exposure.go`, codes above) — disclosed here, not silently dropped. No `contracts/upstream/*` stub covers these legs: the wire-shape generator snapshots forwarded relay spellings only, and no Impact leg has one.

## Explicitly OUT — no suppressions leg is admitted here

The suppressions mutation (oracle `POST /api/v0/supply-chain/impact/suppressions`, `paths/supply/chain/suppression_mutation.go`) is not admitted by this pin and stays Never-satellite: the suppressions mutation row stays the Decision-2 tier-C survivor via `SuppressionAdmission` (recorded in the IaC pin's OUT note), never a read termination here.

Also OUT (each keeps its own standing until a deliberate pin covers it):

- `ChangeSurfaceCodeBackend`, `DeploymentTraceContextProvider`, and `BuildServiceDeploymentOverview` (core logic and adapter seams, not reads; the topic search they exercise IS frozen here, the service-story closure rides the cited Entity pin).
- `PathProbeBackend` methods (the production prober decodes raw driver values past this seam; the anchor resolutions read through the frozen `Run`/`RunSingle` pair).
- `BuildContentRelationships`-style builders and every other `ContentStore` method (no W5 impact path calls them; the trace-leg enrichment tail reads ride the cited Entity pin, not this one).
- `MatchRepositories` and `ListRepositories` (the impact selector legs bind graph rows, never a catalog entry — no catalog shape to freeze).
- Profile, capability gates, grant-before-limit checks, scoped-token filtering, and fixed failure messages (handler behavior, proven at byte-parity time, never frozen).
- No serialization, no worker or batch reduction anywhere in this lane.

## Rollback

Revert this pin. The seam is unwired — no handler, route, or caller depends on it yet — so rollback moves no traffic and changes no data.
