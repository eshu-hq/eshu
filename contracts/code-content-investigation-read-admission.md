# CodeContentInvestigationReadAdmission v0 (core pin record)

Refs eshu-hq/eshu#9669. Wave-4 Code tier-b core pin, per the arbiter
step-2 scope ruling (decisions 2, 6, 7). Minted core-side; the satellite
records this pin in a follow-up. A satellite PR consuming an unminted
pin is invalid. Arbiter review of this pin precedes satellite
termination of the structure-inventory and topics-investigate legs.

## Shape

Version: `CodeContentInvestigationReadAdmissionContractVersion = "v0"`
(go/internal/query/codequery/code_content_investigation_read_admission.go).

Exactly 3 re-declared (never embedded) methods, sealed — no graph
traversal, search, list, secret, ledger, or snapshot surface rides
along:

| # | Method | Mirrors | Serves |
|---|--------|---------|--------|
| 1 | `InspectStructuralInventory` | staying handler port | `POST /api/v0/code/structure/inventory` inventory projection |
| 2 | `CountStructuralInventoryByFile` | staying handler port | structure-inventory per-file counts |
| 3 | `InvestigateCodeTopic` | staying handler port | `POST /api/v0/code/topics/investigate` topic evidence |

Route oracle codes (oracle `8a78eb3f48`,
`paths/code/symbols.go`): `POST /api/v0/code/structure/inventory`
(200/400/403/500/503/504), `POST /api/v0/code/topics/investigate`
(200/400/403/500/503/504).

Transport: no `WithReadStore` constructor on this pin — the
investigation seam admits only the structural-inventory and topic
projections, and minting a store constructor would pick a wiring the
satellite never runs. The compile-time mirror assertion in the pin file
proves the shape the legs run against today satisfies the pin.

Evidence (not enforcement): the wire-shapes proof through the real
`Mount` (`code_content_investigation_read_admission_wire_shapes_test.go`
— `TestCodeContentInvestigationReadAdmissionWireShapesAreSatelliteConsumable`).

Enforcement (all in go/internal/query/codequery):

- `code_content_investigation_read_admission_contract_test.go` —
  frozen method-set test plus version pin
  (`TestCodeContentInvestigationReadAdmissionContractIsVersioned`),
  witness (`codeContentInvestigationReadAdmissionWitness` + satisfier
  assertion) and its re-signed twin
  (`TestCodeContentInvestigationReadAdmissionRejectsResignedMethod`),
  seal
  (`TestCodeContentInvestigationReadAdmissionWireShapesAreSealed` —
  exactly 3 methods, no graph/search/list/secret/ledger/snapshot
  surface), and the seeded-drift RED half
  (`TestCodeContentInvestigationReadAdmissionSeededDriftIsDetected` —
  the planted `Run` widening must not match the frozen set).

## Satellite consumption

Depend on the shape, not a constructor: serve both legs through the
existing projection shape, the same one the legs run against today. A
satellite PR consuming this pin for any other backend shape is invalid
without arbiter review.

## W8a disclosed-absence posture

`POST /api/v0/code/structure/inventory` and `POST
/api/v0/code/topics/investigate` are natively mounted in-tree
(`CodeHandler.Mount`, go/internal/query/codequery/handler.go) and
their OpenAPI fragments stay core-side (oracle
`paths/code/symbols.go`, codes above) — disclosed here, not silently
dropped. No `contracts/upstream/*` stub covers these legs: the
wire-shape generator snapshots forwarded relay spellings only, and no
investigation leg has one.

## Explicitly OUT (not widened by this pin)

- `Run`/`RunSingle` bounded graph reads — the investigation legs admit
  only the two projections. Proven at the seal, not just in the frozen
  set.
- `imports/investigate` — the graph-scan pin covers it (see
  contracts/code-graph-scan-read-admission.md).
- Divergence investigate/report — unpinned legs with their own
  standing; a satellite PR must not cite this pin as their coverage.
- Secrets investigate — the hardcoded-secret pin covers it (see
  contracts/code-hardcoded-secret-read-admission.md).
- Grant checks, fixed failure messages, and `QueryProfile` (handler
  behavior, not port shape).

## Rollback

Revert this pin. The seam is unwired — no handler, route, or caller
depends on it yet — so rollback moves no traffic and changes no data.
