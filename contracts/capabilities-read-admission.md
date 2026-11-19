# CapabilityReadAdmission v0 (core pin record)

Refs eshu-hq/eshu#9669. Wave-5 Capabilities tier-b core pin, per the
arbiter step-2 scope ruling (decisions 2, 6, 7). Minted core-side;
the satellite records this pin in a follow-up. A satellite PR
consuming an unminted pin is invalid. Arbiter review of this pin
precedes satellite termination of the capabilities leg.

## Shape

Version: `CapabilityReadAdmissionContractVersion = "v0"`
(go/internal/query/capability/read_admission.go).

Exactly 1 re-declared (never aliased) read, sealed:

| # | Method | Mirrors | Serves |
|---|--------|---------|--------|
| 1 | `LoadCapabilityCatalog` | `capabilitycatalog.Load` | `GET /api/v0/capabilities` embedded catalog load |

Route oracle codes (oracle `8a78eb3f48`): `GET /api/v0/capabilities`
(`paths/catalog/capabilities.go`: 200/400/500).

Transport: no constructor on this pin. The `capabilityStaticLoader`
adapter delegates the re-declared read to the static catalog load the
handler leg calls today, so removal, rename, or re-signing of `Load`
fails compilation here. Concrete-double-only proof is rejected.

Enforcement (all in go/internal/query/capability):

- `read_admission_contract_test.go` — frozen method-set test plus
  version pin
  (`TestCapabilityReadAdmissionContractIsVersioned`), witness
  (`capabilityReadAdmissionWitness` satisfier assertion), seal
  (`TestCapabilityReadAdmissionWireShapesAreSealed` — exactly 1
  method), and the seeded-drift RED half
  (`TestCapabilityReadAdmissionSeededDriftIsDetected` — the planted
  `seededCapabilityDrift` widening must not match the frozen set).

## Satellite consumption

Depend on the shape, not a constructor: serve the leg through the
existing static catalog load, the same read the leg runs against
today. This is the W5 capabilities family (the single oracle
Capabilities row; the eshu-mcp satellite probes this route to admit
forwarded credentials, eshu-hq/eshu-mcp#50). A satellite PR consuming
this pin for any wider catalog surface is invalid without arbiter
review.

## W8a disclosed-absence posture

`GET /api/v0/capabilities` is natively mounted in-tree and its
OpenAPI fragment stays core-side (oracle
`paths/catalog/capabilities.go`, codes above) — disclosed here, not
silently dropped. No `contracts/upstream/*` stub covers this leg:
the wire-shape generator snapshots forwarded relay spellings only,
and no Capabilities leg has one.

## Explicitly OUT

Also OUT (each keeps its own standing until a deliberate pin covers
it):

- Every other catalog accessor: this pin adopts the embedded catalog
  load only; a new accessor is adopted only by deliberate
  re-declaration plus a frozen-list update.
- Limit/offset parsing and the 400 mapping (handler behavior, not
  port shape).

## Rollback

Revert this pin. The seam is unwired — no handler, route, or caller
depends on it yet — so rollback moves no traffic and changes no data.
