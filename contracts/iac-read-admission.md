# IaCReadAdmission, IaCDeadContentAdmission & IaCInventorySearchAdmission v0 (IaC pin record)

Refs eshu-hq/eshu#9669. Wave-5 IaC tier-b core pin, per the arbiter step-2 scope ruling (decisions 2, 6, 7). Minted core-side; the satellite records this pin in a follow-up. A satellite PR consuming an unminted pin is invalid. Arbiter review of this pin precedes satellite termination of the IaC legs.

## Shape

Versions: `IaCReadAdmissionContractVersion = "v0"` (`go/internal/query/iac/read_admission.go`, 8 methods), `IaCDeadContentAdmissionContractVersion = "v0"` (`go/internal/query/iac/dead_content_admission.go`, 4 methods), `IaCInventorySearchAdmissionContractVersion = "v0"` (`go/internal/query/iac/search_admission.go`, 1 method). Thirteen re-declared (never embedded) methods total, sealed:

| # | Method | Mirrors | Serves |
|---|--------|---------|--------|
| 1 | `Run` (`RunSingle` deliberately excluded) | `querycontract.GraphQuery` | bounded inventory graph page |
| 2 | `Summary` | inventory store | resources facet |
| 3 | `ListReplatformingSelectors` | selector store | selectors leg |
| 4 | `ListUnmanagedCloudResources`, `CountUnmanagedCloudResources` | management store | cloud-management findings |
| 5 | `ListLatestCleanupFindings`, `CountLatestCleanupFindings`, `HasLatestRows` | reachability store | materialized cleanup reads |
| 6 | `ListRepositories`, `MatchRepositories`, `ListRepoFiles`, `GetFileContent` | `querycontract.ContentStore` | dead-route content fallback supplement |
| 7 | `SearchActive` (over the exported `IaCInventorySearch` kind seam) | inventory store | active-inventory search supplement |

Route oracle codes (oracle `8a78eb3f48`): `GET /api/v0/iac/resources` (`paths/iac/resources.go`: 200/400/403/500/501/503/504), `GET /api/v0/replatforming/selectors` (`paths/iac/replatforming_selectors.go`: 200/400/403/500/501/503), `POST /api/v0/aws/runtime-drift/findings` (`paths/cloud/aws_runtime_drift.go`: 200/400/403/500/501/503), `POST /api/v0/iac/dead` (`paths/iac/routes.go`: 200/400/403/500/501/503), `POST /api/v0/iac/unmanaged-resources` (`paths/iac/routes.go`: 200/400/403/500/501/503), `POST /api/v0/iac/terraform-import-plan/candidates` (`paths/iac/routes.go`: 200/400/403/500/501/503), `POST /api/v0/iac/management-status` (`paths/iac/routes.go`: 200/400/403/500/501/503), `POST /api/v0/iac/management-status/explain` (`paths/iac/routes.go`: 200/400/403/500/501/503), `POST /api/v0/replatforming/ownership-packets` (`paths/iac/replatforming_ownership.go`: 200/400/403/500/501/503), `POST /api/v0/replatforming/plans` (`paths/iac/replatforming.go`: 200/400/403/500/501/503), `POST /api/v0/replatforming/rollups` (`paths/iac/replatforming_rollups.go`: 200/400/403/500/501/503).

Transport: no `WithReadStore` constructor on these pins — minting one would pick a wiring the satellite never runs. The compile-time mirror assertions in each pin file prove the stores the legs run against today satisfy the pins. Concrete-double-only proof is rejected. The two supplements ride beside the 8-method pin, never inside it.

Enforcement (all in `go/internal/query/iac/`):

- `read_admission_contract_test.go` — version pin (`TestIaCReadAdmissionContractIsVersioned`), witness (`iaCReadAdmissionWitness`), seal (`TestIaCReadAdmissionWireShapesAreSealed` — exactly the 8-method frozen set), and the seeded-drift RED half (`TestIaCReadAdmissionSeededDriftIsDetected`).
- `dead_content_admission_contract_test.go` — version pin (`TestIaCDeadContentAdmissionContractIsVersioned`), witness (`iaCDeadContentAdmissionWitness` + satisfier assertion), seal (`TestIaCDeadContentAdmissionWireShapesAreSealed` — exactly the 4-method frozen set), and the seeded-drift RED half (`TestIaCDeadContentAdmissionSeededDriftIsDetected`).
- `search_admission_contract_test.go` — version pin (`TestIaCInventorySearchAdmissionContractIsVersioned`), witness (`iacInventorySearchAdmissionWitness` over the Postgres inventory store, unknown kinds rejected at the seam), seal (`TestIaCInventorySearchAdmissionWireShapesAreSealed` — exactly the 1-method frozen set), and the seeded-drift RED half (`TestIaCInventorySearchAdmissionSeededDriftIsDetected`).

## Satellite consumption

Depend on the shape, not a constructor: serve the eleven legs through the same stores the legs run against today (graph + inventory + management + selector + reachability, plus the content store for the dead-route fallback and the Postgres inventory store for the search supplement). A satellite PR consuming this pin for any other backend combination is invalid without arbiter review.

## W8a disclosed-absence posture

All eleven routes are natively mounted in-tree and their OpenAPI fragments stay core-side (oracle `paths/iac/resources.go`, `replatforming_selectors.go`, `routes.go`, `replatforming_ownership.go`, `replatforming.go`, `replatforming_rollups.go`, and `paths/cloud/aws_runtime_drift.go`, codes above) — disclosed here, not silently dropped. No `contracts/upstream/*` stub covers these legs: the wire-shape generator snapshots forwarded relay spellings only, and no IaC leg has one.

## Explicitly OUT — suppressions stay the Decision-2 tier-C survivor

The suppressions mutation row stays the Decision-2 tier-C survivor via `SuppressionAdmission`, never a read termination here. Also OUT (each keeps its own standing until a deliberate pin covers it):

- Grant checks, scope/repo comparisons, `bindIaCManagementFilterAccess`, scoped-empty short-circuits, and fixed failure messages (handler behavior, not port shape).
- `GraphQuery.RunSingle` (deliberately excluded: the resource list hydrates through `Run` only) and `InventoryStore.SearchActive` (adopted only by the search supplement, never the 8-method pin).
- `MetricsReadAdmission` (non-test IaC touches no `TimeSeries` source).
- `InventoryStore` beyond `Summary` + `SearchActive`, and every other shared-port method (adopted only by deliberate re-declaration plus a frozen-list update).

## Rollback

Revert this pin. The seam is unwired — no handler, route, or caller depends on it yet — so rollback moves no traffic and changes no data.
