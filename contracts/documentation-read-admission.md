# DocumentationReadAdmission v0 (documentation pin record)

Refs eshu-hq/eshu#9669. Wave-5 Documentation tier-b core pin, per the arbiter step-2 scope ruling (decisions 2, 6, 7). Minted core-side; the satellite records this pin in a follow-up. A satellite PR consuming an unminted pin is invalid. Arbiter review of this pin precedes satellite termination of the Documentation legs.

## Shape

Version: `DocumentationReadAdmissionContractVersion = "v0"` (`go/internal/query/documentation_read_admission.go`).

Exactly 8 re-declared (never embedded) methods, sealed — no write surface, no other freshness derivation rides along:

| # | Method | Mirrors | Serves |
|---|--------|---------|--------|
| 1 | `DocumentationFindings` | read-model store | findings leg |
| 2 | `DocumentationFacts` | read-model store | facts leg |
| 3 | `DocumentationEvidencePacket`, `DocumentationEvidencePacketWithFilter` | read-model store | evidence-packet leg |
| 4 | `DocumentationEvidencePacketFreshness`, `DocumentationEvidencePacketFreshnessWithFilter` | read-model store | packet-freshness leg |
| 5 | `CountDocumentationFindings` | finding-aggregate store | count leg |
| 6 | `DocumentationFindingInventory` | finding-aggregate store | inventory leg |

Route oracle codes (oracle `8a78eb3f48`): `GET /api/v0/documentation/facts` (`paths/evidence/documentation.go`: 200/400/403/500/501/503), `GET /api/v0/documentation/findings` (`paths/evidence/documentation.go`: 200/400/403/500/501/503), `GET /api/v0/documentation/findings/{finding_id}/evidence-packet` (`paths/evidence/documentation.go`: 200/400/403/404/500/501/503), `GET /api/v0/documentation/evidence-packets/{packet_id}/freshness` (`paths/evidence/documentation.go`: 200/400/403/404/500/501/503), `GET /api/v0/documentation/findings/count` (`paths/evidence/documentation_finding_aggregate.go`: 200/403/500/501/503), `GET /api/v0/documentation/findings/inventory` (`paths/evidence/documentation_finding_aggregate.go`: 200/403/500/501/503).

Transport: no `WithReadStore` constructor on this pin — minting one would pick a wiring the satellite never runs. The compile-time mirror assertion in the pin file proves the content reader and finding-aggregate store the legs run against today satisfy the pin. Concrete-double-only proof is rejected.

Enforcement (all in `go/internal/query/`):

- `documentation_read_admission_contract_test.go` — frozen method-set test plus version pin (`TestDocumentationReadAdmissionContractIsVersioned`), witness (`documentationReadAdmissionWitness` + satisfier assertion), seal (`TestDocumentationReadAdmissionWireShapesAreSealed` — exactly the 8-method frozen set), and the seeded-drift RED half (`TestDocumentationReadAdmissionSeededDriftIsDetected` — a planted widening must not match the frozen set).

## Satellite consumption

Depend on the shape, not a constructor: serve the six legs through the content reader plus the finding-aggregate store, the same backends the legs run against today. A satellite PR consuming this pin for any other backend combination is invalid without arbiter review.

## W8a disclosed-absence posture

All six GETs are natively mounted in-tree and their OpenAPI fragments stay core-side (oracle `paths/evidence/documentation.go` and `documentation_finding_aggregate.go`, codes above) — disclosed here, not silently dropped. No `contracts/upstream/*` stub covers these legs: the wire-shape generator snapshots forwarded relay spellings only, and no Documentation leg has one.

## Explicitly OUT — other freshness derivations stay out

This pin adopts the packet freshness read with its packet-scoped filter variant only. Freshness derivations owned by other lanes (generations, changed-since) stay out: each keeps its own standing until a deliberate pin covers it.

## Rollback

Revert this pin. The seam is unwired — no handler, route, or caller depends on it yet — so rollback moves no traffic and changes no data.
