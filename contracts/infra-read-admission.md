# InfraReadAdmission v0 (infra pin record)

Refs eshu-hq/eshu#9669. Wave-5 Infra tier-b core pin, per the arbiter step-2 scope ruling (decisions 2, 6, 7). Minted core-side; the satellite records this pin in a follow-up. A satellite PR consuming an unminted pin is invalid. Arbiter review of this pin precedes satellite termination of the Infra legs.

## Shape

Version: `InfraReadAdmissionContractVersion = "v0"` (`go/internal/query/infrastructure_read_admission.go`).

Exactly 5 re-declared (never embedded) methods, sealed — no write surface, no wider store surface rides along:

| # | Method | Mirrors | Serves |
|---|--------|---------|--------|
| 1 | `Run`, `RunSingle` | `GraphQuery` | search, relationships, ecosystem, catalog, and edges legs |
| 2 | `ListCloudResourceIdentities` | cloud-resource list store | cloud-resources leg |
| 3 | `CountInfraResources` | infra aggregate store | count leg |
| 4 | `InfraResourceInventory` | infra aggregate store | inventory leg |

Route oracle codes (oracle `8a78eb3f48`): `GET /api/v0/cloud/resources` (`paths/cloud/routes.go`: 200/400/403/500/501/503/504), `GET /api/v0/ecosystem/overview` (`paths/infrastructure/routes.go`: 200/403/500/503/504), `GET /api/v0/infra/resources/count` (`paths/infrastructure/resource_aggregate.go`: 200/403/500/503/504), `GET /api/v0/infra/resources/inventory` (`paths/infrastructure/resource_aggregate.go`: 200/403/500/503/504), `POST /api/v0/ecosystem/graph-summary` (`paths/infrastructure/routes.go`: 200/400/403/500/501/503/504), `POST /api/v0/infra/relationships` (`paths/infrastructure/routes.go`: 200/400/403/404/500/503/504), `POST /api/v0/infra/resources/search` (`paths/infrastructure/routes.go`: 200/400/403/500/503/504), `POST /api/v0/relationships/catalog` (`paths/infrastructure/routes.go`: 200/500/501/503/504), `POST /api/v0/relationships/edges` (`paths/infrastructure/routes.go`: 200/400/403/500/501/503/504).

Transport: no `WithReadStore` constructor on this pin — minting one would pick a wiring the satellite never runs. The compile-time mirror assertion in the pin file proves the graph reader, cloud-resource list store, and infra aggregate store the legs run against today satisfy the pin. Concrete-double-only proof is rejected.

Enforcement (all in `go/internal/query/`):

- `infrastructure_read_admission_contract_test.go` — frozen method-set test plus version pin (`TestInfraReadAdmissionContractIsVersioned`), witness (`infraReadAdmissionWitness` + satisfier assertion), seal (`TestInfraReadAdmissionWireShapesAreSealed` — exactly the 5-method frozen set), and the seeded-drift RED half (`TestInfraReadAdmissionSeededDriftIsDetected` — a planted widening must not match the frozen set).

## Satellite consumption

Depend on the shape, not a constructor: serve the nine legs through the graph reader plus the cloud-resource list and infra aggregate stores, the same backends the legs run against today. A satellite PR consuming this pin for any other backend combination is invalid without arbiter review.

## W8a disclosed-absence posture

All nine routes are natively mounted in-tree and their OpenAPI fragments stay core-side (oracle `paths/cloud/routes.go`, `paths/infrastructure/routes.go`, and `resource_aggregate.go`, codes above) — disclosed here, not silently dropped. No `contracts/upstream/*` stub covers these legs: the wire-shape generator snapshots forwarded relay spellings only, and no Infra leg has one.

## Explicitly OUT — no carve-out: every Infra row is admitted, nothing else is

All 9 oracle Infra rows are admitted by this pin. Nothing beyond the five reads is adopted: a new method on any mirrored store is adopted only by deliberate re-declaration plus a frozen-list update, and every other store consumer keeps its own per-route relay reason until deliberate pins cover them.

## Rollback

Revert this pin. The seam is unwired — no handler, route, or caller depends on it yet — so rollback moves no traffic and changes no data.
