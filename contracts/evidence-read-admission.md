# EvidenceReadAdmission v0 (evidence pin record)

Refs eshu-hq/eshu#9669. Wave-5 Evidence tier-b core pin, per the arbiter step-2 scope ruling (decisions 2, 6, 7). Minted core-side; the satellite records this pin in a follow-up. A satellite PR consuming an unminted pin is invalid. Arbiter review of this pin precedes satellite termination of the Evidence legs.

## Shape

Version: `EvidenceReadAdmissionContractVersion = "v0"` (`go/internal/query/evidence/read_admission.go`).

Exactly 7 re-declared (never embedded) methods, sealed — no responder seam, no write surface rides along:

| # | Method | Mirrors | Serves |
|---|--------|---------|--------|
| 1 | `RelationshipEvidenceByResolvedID` | relationship-evidence store | relationship drilldown leg |
| 2 | `ListAdmissionDecisions`, `ListAdmissionDecisionEvidence` | decision store | admission-decisions and deployable-unit packet legs |
| 3 | `EvidenceCitationFiles`, `GetFileContent` | citation store + `ContentStore` | citations leg (plus the content fallback) |
| 4 | `ReadStatusSnapshotFiltered` | status reader | bundle leg |
| 5 | `RunSingle` | `GraphQuery` | repository count on the bundle leg |

Route oracle codes (oracle `8a78eb3f48`): `GET /api/v0/evidence/relationships/{resolved_id}` (`paths/evidence/routes.go`: 200/400/403/404/500/501/503), `GET /api/v0/evidence/admission-decisions` (`paths/evidence/routes.go`: 200/400/403/500/501/503), `POST /api/v0/evidence/citations` (`paths/evidence/routes.go`: 200/400/403/500/501/503), `GET /api/v0/evidence/bundle` (`paths/evidence/bundle.go`: 200/500/503), `GET /api/v0/investigations/deployable-unit/packet` (`paths/evidence/investigations.go`: 200/400/403/500/501/503).

Transport: no `WithReadStore` constructor on this pin — minting one would pick a wiring the satellite never runs. The compile-time mirror assertion in the pin file proves the content reader, decision store, citation store, status reader, and graph reader the legs run against today satisfy the pin. Concrete-double-only proof is rejected.

Enforcement (all in `go/internal/query/evidence/`):

- `read_admission_contract_test.go` — frozen method-set test plus version pin (`TestEvidenceReadAdmissionContractIsVersioned`), witness (`evidenceReadAdmissionWitness` + satisfier assertion), seal (`TestEvidenceReadAdmissionWireShapesAreSealed` — exactly the 7-method frozen set), and the seeded-drift RED half (`TestEvidenceReadAdmissionSeededDriftIsDetected` — a planted widening must not match the frozen set).

## Satellite consumption

Depend on the shape, not a constructor: serve the five legs through the same stores the legs run against today. A satellite PR consuming this pin for any other backend combination is invalid without arbiter review.

## W8a disclosed-absence posture

All five routes are natively mounted in-tree and their OpenAPI fragments stay core-side (oracle `paths/evidence/routes.go`, `bundle.go`, and `investigations.go`, codes above) — disclosed here, not silently dropped. No `contracts/upstream/*` stub covers these legs: the wire-shape generator snapshots forwarded relay spellings only, and no Evidence leg has one.

## Explicitly OUT — the packet responder seam is handler behavior

The deployable-unit packet composes through the packet responder seam (an HTTP-writing seam, not a read): the packet leg admits its decision read through `ListAdmissionDecisions` above, and the responder stays handler behavior, not port shape — there is no method set to freeze for it.

## Rollback

Revert this pin. The seam is unwired — no handler, route, or caller depends on it yet — so rollback moves no traffic and changes no data.
