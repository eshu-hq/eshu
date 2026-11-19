# TagHistoryReadAdmission v0 (core pin record)

Refs eshu-hq/eshu#9669. Wave-5 TagHistory tier-b core pin, per the
arbiter step-2 scope ruling (decisions 2, 6, 7). Minted core-side;
the satellite records this pin in a follow-up. A satellite PR
consuming an unminted pin is invalid. Arbiter review of this pin
precedes satellite termination of the tag-history leg.

## Shape

Version: `TagHistoryReadAdmissionContractVersion = "v0"`
(go/internal/query/tag_history_read_admission.go).

Exactly 1 re-declared (never embedded) method, sealed — issued once
unscoped and up to 8 times scoped under one shared bounded-read
deadline:

| # | Method | Mirrors | Serves |
|---|--------|---------|--------|
| 1 | `Run` | `querycontract.GraphQuery` | `GET /api/v0/images/tag-history` bounded graph read |

Route oracle codes (oracle `8a78eb3f48`): `GET
/api/v0/images/tag-history` (`paths/supply/chain/tag_history.go`:
200/400/403/500/501/503/504).

Transport: no constructor, selector, or `ReadStore` surface on this
pin. The compile-time mirror assertion keeps the re-declaration
honest: removal, rename, or re-signing of `Run` on the staying
handler-facing port fails compilation here. Duplicate re-declaration
of `Run` across the W8b Run-only pins is correct, not waste: each pin
freezes its own family shape. Concrete-double-only proof is rejected.

Enforcement (all in go/internal/query):

- `tag_history_read_admission_contract_test.go` — frozen method-set
  test plus version pin
  (`TestTagHistoryReadAdmissionContractIsVersioned`), seal
  (`TestTagHistoryReadAdmissionWireShapesAreSealed` — exactly 1
  method), and the seeded-drift RED half
  (`TestTagHistoryReadAdmissionSeededDriftIsDetected` — the planted
  `seededTagHistoryDrift` widening must not match the frozen set).

## Satellite consumption

Depend on the shape, not a constructor: serve the leg through the
existing graph reader, the same read the leg runs against today. This
is the W8b-graph2 leaf, per the arbiter SUPPLEMENTAL-PIN-OWED ruling
on eshu-hq/eshu-api#101. Do not merge this pin with
ImageReadAdmission: different handler, capability, and grant model. A
satellite PR consuming this pin for any other backend is invalid
without arbiter review.

## W8a disclosed-absence posture

`GET /api/v0/images/tag-history` is natively mounted in-tree and its
OpenAPI fragment stays core-side (oracle
`paths/supply/chain/tag_history.go`, codes above) — disclosed here,
not silently dropped. No `contracts/upstream/*` stub covers this leg:
the wire-shape generator snapshots forwarded relay spellings only,
and no TagHistory leg has one.

## Explicitly OUT

Also OUT (each keeps its own standing until a deliberate pin covers
it):

- `GraphQuery.RunSingle` stays unpinned by construction: the
  tag-history lane never issues a single-row graph read.
- The taghistory.Sealer (DEK cursor sealing) is config/crypto, not
  port shape: nil-sealer fail-closed behavior is handler behavior the
  parity suite must drive.
- The grant checks, the fixed failure messages, and `QueryProfile`
  (handler behavior, not port shape).

## Rollback

Revert this pin. The seam is unwired — no handler, route, or caller
depends on it yet — so rollback moves no traffic and changes no data.
