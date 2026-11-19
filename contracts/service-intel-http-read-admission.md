# ServiceIntelHTTPReadAdmission v0 (W5 pin record)

Refs eshu-hq/eshu#9669. Wave-5 ServiceIntelHTTP tier-b core pin, per the
arbiter step-2 scope ruling (decisions 2, 6, 7). Minted core-side; the
satellite records this pin in a follow-up. A satellite PR consuming an
unminted pin is invalid. Arbiter review of this pin precedes satellite
termination of the intelligence-report legs.

## Shape

Version: `ServiceIntelHTTPReadAdmissionContractVersion = "v0"`
(go/internal/serviceintelhttp/read_admission.go).

Exactly 4 reads, re-declared in this service-intelligence-scoped shape
instead of embedding the entity Handler or the source interfaces — the
story, context, and downstream legs keep their own entity-pin standing:

| # | Method | Mirrors | Serves |
|---|--------|---------|--------|
| 1 | `BuildServiceStoryEnvelope` | entity `Handler` seam (dossier leg) | `GET /api/v0/services/{service_name}/intelligence-report` (dossier) |
| 2 | `IncidentRecordsForWorkload` | `IncidentEvidenceSource` | `GET /api/v0/services/{service_name}/intelligence-report` (incidents_support) |
| 3 | `SupplyChainInventoryForWorkload` | `SupplyChainEvidenceSource` | `GET /api/v0/services/{service_name}/intelligence-report` (supply_chain) |
| 4 | `ComposeServiceIntelReport` | `serviceintel.Compose` | `GET /api/v0/services/{service_name}/intelligence-report` (compose) |

Route oracle codes (oracle `8a78eb3f48`): `GET
/api/v0/services/{service_name}/intelligence-report`
(`paths/service/intelligence_report.go`: 200/400/403/404/409/500/501).

Transport: no constructor on this pin — the compile-time adapter assertion
proves the production-shaped deriver composes the entity builder, the two
evidence sources, and the static composer exactly the way the handler leg
wires them, and the entity `Handler` satisfies the dossier seam. This pin
lives in package `serviceintelhttp` because the handler lives outside
`internal/query`; the no-new-tree rule is met — the pin sits beside the
handler it covers, the same placement the query-tree pins use.
Concrete-double-only proof is rejected.

Enforcement (all in go/internal/serviceintelhttp):

- `read_admission_contract_test.go` — frozen method-set test plus version
  pin (`TestServiceIntelHTTPReadAdmissionContractIsVersioned`), witness
  (`serviceIntelHTTPReadAdmissionWitness` plus satisfier assertion), seal
  (`TestServiceIntelHTTPReadAdmissionWireShapesAreSealed` — exactly 4
  reads), and the seeded-drift RED half
  (`TestServiceIntelHTTPReadAdmissionSeededDriftIsDetected` — a planted
  widening must not match the frozen set).

## Satellite consumption

Depend on the shape, not a constructor: serve the intelligence-report leg
through the pin, composed by the consumer from the same dossier builder,
evidence sources, and static composer the leg runs against today. A
satellite PR consuming this pin for any other service-story consumer is
invalid without arbiter review.

## W8a disclosed-absence posture

`GET /api/v0/services/{service_name}/intelligence-report` is natively
mounted in-tree and its OpenAPI fragment stays core-side (oracle
`paths/service/intelligence_report.go`, codes above) — disclosed here, not
silently dropped. No `contracts/upstream/*` stub covers this leg: the
wire-shape generator snapshots forwarded relay spellings only, and no
ServiceIntelHTTP leg has one.

## Explicitly OUT

- The other service-story consumers (story, context, downstream legs) keep
  their own entity-pin standing: this pin grants nothing to them, and a
  new source in this package widens nothing until deliberate
  re-declaration plus a frozen-list update.
- Grant checks, failure mappings, and truth-envelope shaping are handler
  behavior, not port shape: there is no method set to freeze for them.

## Rollback

Revert this pin. The seam is unwired — no handler, route, or caller depends
on it yet — so rollback moves no traffic and changes no data.
