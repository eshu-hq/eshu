# PackageRegistryReadAdmission v0 (package-registry pin record)

Refs eshu-hq/eshu#9669. Wave-5 PackageRegistry tier-b core pin, per the arbiter step-2 scope ruling (decisions 2, 6, 7). Minted core-side; the satellite records this pin in a follow-up. A satellite PR consuming an unminted pin is invalid. Arbiter review of this pin precedes satellite termination of the PackageRegistry legs.

## Shape

Version: `PackageRegistryReadAdmissionContractVersion = "v0"` (`go/internal/query/package/registry/read_admission.go`).

Exactly 4 re-declared (never embedded) methods, sealed — no single-row graph read, no content read, no readiness probe rides along:

| # | Method | Mirrors | Serves |
|---|--------|---------|--------|
| 1 | `Run` | `querycontract.GraphQuery` | bounded graph reads on the registry legs |
| 2 | `ListPackageRegistryCorrelations` | correlation store | correlations leg |
| 3 | `CountPackageRegistryPackages` | aggregate store | count leg |
| 4 | `PackageRegistryPackageInventory` | aggregate store | inventory leg |

Route oracle codes (oracle `8a78eb3f48`): `GET /api/v0/package-registry/packages` (`paths/repository/package_registry.go`: 200/400/403/500/501/503/504), `GET /api/v0/package-registry/versions` (`paths/repository/package_registry.go`: 200/400/403/500/501/503/504), `GET /api/v0/package-registry/dependencies` (`paths/repository/package_registry.go`: 200/400/403/500/501/503/504), `GET /api/v0/package-registry/correlations` (`paths/repository/package_registry.go`: 200/400/403/500/501/503/504), `GET /api/v0/package-registry/dependency-chains` (`paths/repository/package_registry.go`: 200/400/403/500/501/503/504), `GET /api/v0/package-registry/packages/count` (`paths/repository/package_registry_aggregate.go`: 200/403/503/504), `GET /api/v0/package-registry/packages/inventory` (`paths/repository/package_registry_aggregate.go`: 200/403/503/504).

Transport: no `WithReadStore` constructor on this pin — minting one would pick a wiring the satellite never runs. The compile-time mirror assertion in the pin file proves the graph reader, correlation store, and aggregate store the legs run against today satisfy the pin. Concrete-double-only proof is rejected.

Enforcement (all in `go/internal/query/package/registry/`):

- `read_admission_contract_test.go` — frozen method-set test plus version pin (`TestPackageRegistryReadAdmissionContractIsVersioned`), witness (`packageRegistryReadAdmissionWitness` + satisfier assertion), seal (`TestPackageRegistryReadAdmissionWireShapesAreSealed` — exactly the 4-method frozen set, no `RunSingle`/content/probe surface), and the seeded-drift RED half (`TestPackageRegistryReadAdmissionSeededDriftIsDetected` — a planted widening must not match the frozen set).

## Satellite consumption

Depend on the shape, not a constructor: serve the seven legs through the graph reader plus the correlation and aggregate stores, the same backends the legs run against today. A satellite PR consuming this pin for any other backend combination is invalid without arbiter review.

## W8a disclosed-absence posture

All seven GETs are natively mounted in-tree and their OpenAPI fragments stay core-side (oracle `paths/repository/package_registry.go` and `package_registry_aggregate.go`, codes above) — disclosed here, not silently dropped. No `contracts/upstream/*` stub covers these legs: the wire-shape generator snapshots forwarded relay spellings only, and no PackageRegistry leg has one.

## Explicitly OUT — `RunSingle`, the Content handle, and the CollectorReadiness probe

`RunSingle` stays unpinned here by construction: no package-registry leg issues a single-row graph read. The Content handle and the `CollectorReadiness` probe are handler behavior, not port shape: the legs exercise no content read, and the readiness probe is an optional envelope, not an admitted read — there is no method set to freeze for either.

## Rollback

Revert this pin. The seam is unwired — no handler, route, or caller depends on it yet — so rollback moves no traffic and changes no data.
