# BrowserSessionAdmission v0 (D8 pin record)

Refs eshu-hq/eshu#9669. Wave-5 SessionList tier-b core pin, per the arbiter
step-2 scope ruling (decisions 2, 6, 7). Minted core-side; the satellite
records this pin in a follow-up. A satellite PR consuming an unminted pin is
invalid. Arbiter review of this pin precedes satellite termination of the
session-list legs.

## Shape

Version: `BrowserSessionAdmissionContractVersion = "v0"`
(go/internal/query/session_admission.go).

D8 standing, recorded explicitly: the pin embeds the narrow session
lifecycle store and adds the metadata-only list read, for 4 methods total.
Wave 5 adopts `ListSessionsBySubject` only — the 3 lifecycle methods ride
the same store under D8 standing and are not Wave-5 scope:

| # | Method | Standing | Serves |
|---|--------|----------|--------|
| 1 | `CreateBrowserSession` | D8 lifecycle (via embedded `session.BrowserSessionStore`) | browser-session lifecycle, not the Wave-5 list leg |
| 2 | `RevokeBrowserSession` | D8 lifecycle (via embedded `session.BrowserSessionStore`) | browser-session lifecycle, not the Wave-5 list leg |
| 3 | `SwitchBrowserSessionWorkspace` | D8 lifecycle (via embedded `session.BrowserSessionStore`) | browser-session context leg, not the Wave-5 list leg |
| 4 | `ListSessionsBySubject` | Wave-5 adoption (metadata-only list read) | `GET /api/v0/auth/sessions` |

The list read stays metadata-only on purpose: rows never carry session
hashes, CSRF hashes, or external auth secrets — the store computes the
`Current` flag by hash comparison (`sessionHash` is server-side input, never
selected or returned), so a dest consumer serving session reads cannot steal
a session through this pin.

Route oracle codes (oracle `8a78eb3f48`): `GET /api/v0/auth/sessions`
(`paths/auth/routes.go`: 200/401/500/503).

Transport: no constructor on this pin — the boundary is
identity-verified by the frozen contract test rather than a parallel
hand-maintained shape. The handler package wins for the list read the same
way it does for the incident pin.

Enforcement (all in go/internal/query):

- `session_admission_contract_test.go` — frozen method-set test plus
  version pin (`TestBrowserSessionAdmissionContractIsVersioned`, over
  `frozenBrowserSessionAdmission`), seal
  (`TestBrowserSessionAdmissionWireShapesAreSealed` — exactly 4 methods,
  no wider surface), and the seeded-drift RED half
  (`TestBrowserSessionAdmissionSeededDriftIsDetected` — the planted
  `PurgeBrowserSessions` widening must not match the frozen set).

## Satellite consumption

Depend on the shape: serve `GET /api/v0/auth/sessions` through
`ListSessionsBySubject`, backed by the same Postgres adapter the D8 legs
run against today. The lifecycle methods come with the embed — a dest
consumer implementing this contract can also run the D8 lifecycle, which is
D8 standing, not a Wave-5 grant. A satellite PR consuming this pin for any
other family is invalid without arbiter review.

## W8a disclosed-absence posture

`GET /api/v0/auth/sessions` is natively mounted in-tree and its OpenAPI
fragment stays core-side (oracle `paths/auth/routes.go`, codes above) —
disclosed here, not silently dropped. No `contracts/upstream/*` stub covers
this leg: the wire-shape generator snapshots forwarded relay spellings
only, and no SessionList leg has one.

## Explicitly OUT

- The session lifecycle (create, revoke, workspace switch) keeps its D8
  standing: Wave 5 adopts the list read only, and a satellite PR citing
  this record as lifecycle-leg coverage beyond the D8 lane is invalid.
- The sibling D8 pins (`SessionResolverAdmission`,
  `LocalIdentityAdmission`, `APITokenAdmission`) are not adopted here: each
  keeps its own standing until a deliberate pin covers it.

## Rollback

Revert the Wave-5 adoption: the `ListSessionsBySubject` re-declaration, its
frozen-list entries, and this record — a test-plus-doc revert. The D8
lifecycle store keeps its standing untouched: rollback removes no lifecycle
method, moves no traffic, and changes no data.
