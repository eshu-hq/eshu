# CICDReadAdmission v0 (core pin record)

Refs eshu-hq/eshu#9669. Wave-5 CICD tier-b core pin, per the arbiter
step-2 scope ruling (decisions 2, 6, 7). Minted core-side; the satellite
records this pin in a follow-up. A satellite PR consuming an unminted
pin is invalid. Arbiter review of this pin precedes satellite
termination of the CICD legs.

## Shape

Version: `CICDReadAdmissionContractVersion = "v0"`
(go/internal/query/cicd/read_admission.go).

Exactly 3 re-declared (never embedded) methods, sealed — no wider
correlation or aggregate surface rides along:

| # | Method | Mirrors | Serves |
|---|--------|---------|--------|
| 1 | `ListCICDRunCorrelations` | `querycontract.CICDRunCorrelationStore` | `GET /api/v0/ci-cd/run-correlations` bounded list |
| 2 | `CountRunCorrelations` | `RunCorrelationAggregateStore` | `GET /api/v0/ci-cd/run-correlations/count` cheap summary |
| 3 | `RunCorrelationInventory` | `RunCorrelationAggregateStore` | `GET /api/v0/ci-cd/run-correlations/inventory` dimensioned summary |

Route oracle codes (oracle `8a78eb3f48`): `GET
/api/v0/ci-cd/run-correlations` (`paths/cicd/routes.go`:
200/400/403/500/501/503), `GET /api/v0/ci-cd/run-correlations/count`
(`paths/cicd/run_correlation_aggregate.go`: 200/403/503), `GET
/api/v0/ci-cd/run-correlations/inventory`
(`paths/cicd/run_correlation_aggregate.go`: 200/403/503).

Transport: no `WithReadStore` constructor on this pin — the CICD seam
spans the correlation store plus the run aggregate store, satisfied
jointly and composed by the consumer. The compile-time mirror
assertion in the pin file proves the pair the legs run against today
satisfies the pin. Concrete-double-only proof is rejected.

Enforcement (all in go/internal/query/cicd):

- `read_admission_contract_test.go` — frozen method-set test plus
  version pin
  (`TestCICDReadAdmissionContractIsVersioned`), witness
  (`cicdReadAdmissionWitness` satisfier assertion), seal
  (`TestCICDReadAdmissionWireShapesAreSealed` — exactly 3 methods),
  and the seeded-drift RED half
  (`TestCICDReadAdmissionSeededDriftIsDetected` — the planted
  widening must not match the frozen set).

## Satellite consumption

Depend on the shape, not a constructor: serve the three legs through
the existing correlation store plus the run aggregate store, the same
pair the legs run against today. A satellite PR consuming this pin
for any other backend pair is invalid without arbiter review.

## W8a disclosed-absence posture

`GET /api/v0/ci-cd/run-correlations`, `GET
/api/v0/ci-cd/run-correlations/count`, and `GET
/api/v0/ci-cd/run-correlations/inventory` are natively mounted
in-tree and their OpenAPI fragments stay core-side (oracle
`paths/cicd/routes.go` and
`paths/cicd/run_correlation_aggregate.go`, codes above) — disclosed
here, not silently dropped. No `contracts/upstream/*` stub covers
these legs: the wire-shape generator snapshots forwarded relay
spellings only, and no CICD leg has one.

## Explicitly OUT

Also OUT (each keeps its own standing until a deliberate pin covers
it):

- Every other store method: this pin adopts
  `ListCICDRunCorrelations`, `CountRunCorrelations`, and
  `RunCorrelationInventory` only; a new shared-port method is adopted
  only by deliberate re-declaration plus a frozen-list update.
- Grant checks, fixed failure messages, and `QueryProfile`
  (handler behavior, not port shape).

## Rollback

Revert this pin. The seam is unwired — no handler, route, or caller
depends on it yet — so rollback moves no traffic and changes no data.
