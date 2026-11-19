# VisualizationReadAdmission v0 (G2 pin record)

Refs eshu-hq/eshu#9669. Wave-5 Visualization tier-b core pin, per the arbiter
step-2 scope ruling (decisions 2, 6, 7). Minted core-side; the satellite
records this pin in a follow-up. A satellite PR consuming an unminted pin is
invalid. Arbiter review of this pin precedes satellite termination of the
visualization legs.

## Shape

Version: `VisualizationReadAdmissionContractVersion = "v0"`
(go/internal/query/visualization/read_admission.go).

Exactly 3 builders, re-declared whole in this visualization-scoped shape
instead of embedding a shared port — no list, catalog, graph-results, or
other-family surface rides along:

| # | Method | Mirrors | Serves |
|---|--------|---------|--------|
| 1 | `BuildServiceStoryPacket` | static `BuildServiceStoryPacket` | `POST /api/v0/visualizations/derive` (service-story view) |
| 2 | `BuildEvidenceCitationPacket` | static `BuildEvidenceCitationPacket` | `POST /api/v0/visualizations/derive` (evidence-citation view) |
| 3 | `BuildIncidentContextPacket` | static `BuildIncidentContextPacket` | `POST /api/v0/visualizations/derive` (incident-context view) |

Route oracle codes (oracle `8a78eb3f48`): `POST
/api/v0/visualizations/derive` (`paths/evidence/visualization_packets.go`:
200/400).

Transport: this pin is handler-wired — `Handler.Reads`
(`go/internal/query/visualization/handler.go`) serves the derive dispatch
through the pin, and a nil `Reads` keeps the standing static deriver (the
empty-struct handler takes no store). The compile-time adapter assertion in
the pin file proves the static deriver satisfies the pin. The pin carries
the derive shape only: no constructor, selector, or store surface.

Enforcement (all in go/internal/query/visualization):

- `read_admission_contract_test.go` — frozen method-set test plus version
  pin (`TestVisualizationReadAdmissionContractIsVersioned`), seal
  (`TestVisualizationReadAdmissionWireShapesAreSealed` — exactly 3
  builders), serve-the-route
  (`TestVisualizationReadAdmissionWireShapesServeTheRoute`), and the
  seeded-drift RED half
  (`TestVisualizationReadAdmissionSeededDriftIsDetected` — the planted
  `FromMap` neighbor widening must not match the frozen set).
- `read_admission_wire_test.go` —
  (`TestHandlerDeriveServesThroughPin`): a canned deriver injected through
  `Reads` drives every view through the mounted handler, a handler built
  without an admission keeps deriving through the static default, and an
  unknown view rejects before any builder runs.

## Satellite consumption

Depend on the shape, not a constructor: serve the derive leg through the
pin, the same dispatch the leg runs against today. A satellite PR consuming
this pin for any other view or family is invalid without arbiter review.

## W8a disclosed-absence posture

`POST /api/v0/visualizations/derive` is natively mounted in-tree and its
OpenAPI fragment stays core-side (oracle
`paths/evidence/visualization_packets.go`, codes above) — disclosed here,
not silently dropped. No `contracts/upstream/*` stub covers this leg: the
wire-shape generator snapshots forwarded relay spellings only, and no
Visualization leg has one.

## Explicitly OUT

- The `ViewGraphQuery` executed-Cypher-result view stays unpinned here by
  construction: the derive dispatch never issues it (the switch covers
  service story, evidence citation, and incident context only), so a
  graph-results leg cannot ride this pin.
- The `BuildEvidenceCitationPacketFromMap` and
  `BuildIncidentContextPacketFromMap` transport adapters stay unpinned: the
  dispatch decodes through its own unexported path, so the seeded-drift
  test plants exactly one `FromMap` neighbor to prove the guard sees it.
- The JSON decode, the view allow-list error, and the truth derivation are
  handler behavior, not port shape: there is no method set to freeze.
- No constructor, selector, or store surface: the satellite satisfies this
  port with its own wiring; this pin carries the derive shape only.

## Rollback

Revert this pin. The seam is unwired — no handler, route, or caller depends
on it yet — so rollback moves no traffic and changes no data.
