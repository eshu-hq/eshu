# CloudInventoryReadAdmission v0 (core pin record)

Refs eshu-hq/eshu#9669. Wave-5 CloudInventory tier-b core pin, per the
arbiter step-2 scope ruling (decisions 2, 6, 7). Minted core-side; the
satellite records this pin in a follow-up. A satellite PR consuming an
unminted pin is invalid. Arbiter review of this pin precedes satellite
termination of the inventory leg.

## Shape

Version: `CloudInventoryReadAdmissionContractVersion = "v0"`
(go/internal/query/cloud_inventory_read_admission.go).

Exactly 2 re-declared (never embedded) methods, sealed — the probe is
IN, not OUT: listInventory calls it whenever an account-alias filter
matches zero rows, and that call changes warning_flags bytes:

| # | Method | Mirrors | Serves |
|---|--------|---------|--------|
| 1 | `CloudInventoryIdentities` | staying `cloudInventoryReadModelStore` | `GET /api/v0/cloud/inventory` canonical-identity read |
| 2 | `CloudInventoryPreRolloutEvidenceExists` | staying `cloudInventoryReadModelStore` | same leg, conditional pre-rollout probe |

Route oracle codes (oracle `8a78eb3f48`): `GET
/api/v0/cloud/inventory` (`paths/cloud/inventory.go`:
200/400/403/500/501/503).

Transport: no constructor, selector, or `ReadStore` surface on this
pin — the satellite satisfies it with its own wiring (today the
handler type-asserts `h.Content`). The compile-time mirror assertion
keeps the re-declaration honest and the satisfier assertion
(`*ContentReader`) proves the pinned reads serve with the minimal
shape. Concrete-double-only proof is rejected.

Enforcement (all in go/internal/query):

- `cloud_inventory_read_admission_contract_test.go` — frozen
  method-set test plus version pin
  (`TestCloudInventoryReadAdmissionContractIsVersioned`), seal
  (`TestCloudInventoryReadAdmissionWireShapesAreSealed` — exactly 2
  methods), serve-the-route
  (`TestCloudInventoryReadAdmissionWireShapesServeTheRoute` — both
  pinned reads answer through the pin-typed
  `cloudInventoryAdmissionCannedStore`), and the seeded-drift RED
  half (`TestCloudInventoryReadAdmissionSeededDriftIsDetected`).

## Satellite consumption

Depend on the shape, not a constructor: serve the leg through the
existing content reader, the same read the leg runs against today.
This is the W8b-wide2 leaf, per the arbiter SUPPLEMENTAL-PIN-OWED
ruling on eshu-hq/eshu-api#101. A satellite PR consuming this pin for
any other backend is invalid without arbiter review.

## W8a disclosed-absence posture

`GET /api/v0/cloud/inventory` is natively mounted in-tree and its
OpenAPI fragment stays core-side (oracle
`paths/cloud/inventory.go`, codes above) — disclosed here, not
silently dropped. No `contracts/upstream/*` stub covers this leg:
the wire-shape generator snapshots forwarded relay spellings only,
and no CloudInventory leg has one.

## Explicitly OUT

Also OUT (each keeps its own standing until a deliberate pin covers
it):

- The grant checks, the fixed failure messages, the bounds, and
  `QueryProfile` (handler behavior, not port shape).
- No constructor, selector, or `ReadStore` surface rides along.

## Rollback

Revert this pin. The seam is unwired — no handler, route, or caller
depends on it yet — so rollback moves no traffic and changes no data.
