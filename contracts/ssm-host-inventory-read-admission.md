# SSMHostInventoryReadAdmission v0 (core pin record)

Refs eshu-hq/eshu#9669. Wave-5 SSMHostInventory tier-b core pin, per
the arbiter step-2 scope ruling (decisions 2, 6, 7). Minted
core-side; the satellite records this pin in a follow-up. A satellite
PR consuming an unminted pin is invalid. Arbiter review of this pin
precedes satellite termination of the SSM leg.

## Shape

Version: `SSMHostInventoryReadAdmissionContractVersion = "v0"`
(go/internal/query/ssm_host_inventory_read_admission.go).

Exactly 2 re-declared (never embedded) methods, sealed — the admitted
facts plus the bounded graph read (2 seed reads plus 2 node reads,
skipped when Graph is nil or the page is empty):

| # | Method | Mirrors | Serves |
|---|--------|---------|--------|
| 1 | `SSMHostActiveFacts` | facts half (`*ContentReader`) | `GET /api/v0/cloud/ssm-host-inventory` admitted-facts read |
| 2 | `Run` | graph half (`querycontract.GraphQuery`) | same leg, bounded graph read |

Route oracle codes (oracle `8a78eb3f48`): `GET
/api/v0/cloud/ssm-host-inventory`
(`paths/cloud/ssm_host_inventory.go`: 200/400/403/501/503).

Transport: no constructor, selector, or `ReadStore` surface on this
pin. The unexported `ssmHostInventoryReadAdmissionMirror` keeps both
halves honest in one assertion (the DeadCode witness-struct
precedent): the (GraphQuery + *ContentReader) pair the handler runs
against today satisfies the pin. Concrete-double-only proof is
rejected.

Enforcement (all in go/internal/query):

- `ssm_host_inventory_read_admission_contract_test.go` — frozen
  method-set test plus version pin
  (`TestSSMHostInventoryReadAdmissionContractIsVersioned`), seal
  (`TestSSMHostInventoryReadAdmissionWireShapesAreSealed` — exactly
  2 methods), serve-the-route
  (`TestSSMHostInventoryReadAdmissionWireShapesServeTheRoute` — both
  pinned reads answer through the pin-typed
  `ssmHostInventoryAdmissionCannedStore`), and the seeded-drift RED
  half (`TestSSMHostInventoryReadAdmissionSeededDriftIsDetected`).

## Satellite consumption

Depend on the shape, not a constructor: serve the leg through the
existing graph reader plus the content reader, the same pair the leg
runs against today. This is the W8b-wide2 leaf, per the arbiter
SUPPLEMENTAL-PIN-OWED ruling on eshu-hq/eshu-api#101. A satellite PR
consuming this pin for any other backend pair is invalid without
arbiter review.

## W8a disclosed-absence posture

`GET /api/v0/cloud/ssm-host-inventory` is natively mounted in-tree
and its OpenAPI fragment stays core-side (oracle
`paths/cloud/ssm_host_inventory.go`, codes above) — disclosed here,
not silently dropped. No `contracts/upstream/*` stub covers this leg:
the wire-shape generator snapshots forwarded relay spellings only,
and no SSMHostInventory leg has one.

## Explicitly OUT

Also OUT (each keeps its own standing until a deliberate pin covers
it):

- `GraphQuery.RunSingle` stays unpinned by construction: the SSM lane
  never issues a single-row graph read.
- `ssmhost.Bounds` validation and Now (config/clock, not port
  shape); the zero-bounds 503 is handler behavior the parity suite
  must drive.
- The grant checks, the fixed failure messages, and `QueryProfile`
  (handler behavior, not port shape).

## Rollback

Revert this pin. The seam is unwired — no handler, route, or caller
depends on it yet — so rollback moves no traffic and changes no data.
