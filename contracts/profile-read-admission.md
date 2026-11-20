# ProfileReadAdmission v0 (W5 pin record)

Refs eshu-hq/eshu#9669. Wave-5 Profile tier-b core pin, per the arbiter
step-2 scope ruling (decisions 2, 6, 7). Minted core-side; the satellite
records this pin in a follow-up. A satellite PR consuming an unminted pin is
invalid. Arbiter review of this pin precedes satellite termination of the
profile legs.

## Shape

Version: `ProfileReadAdmissionContractVersion = "v0"`
(go/internal/query/profile_read_admission.go).

Exactly 1 read, re-declared in this profile-read-scoped shape instead of
embedding or aliasing `local.IdentityProfileLister` — every other lister
consumer keeps its own per-route standing:

| # | Method | Mirrors | Serves |
|---|--------|---------|--------|
| 1 | `GetLocalIdentityMFAStatus` | `local.IdentityProfileLister` (as exercised through the profile leg) | `GET /api/v0/auth/profile` |

Route oracle codes (oracle `8a78eb3f48`): `GET /api/v0/auth/profile`
(`paths/auth/routes.go`: 200/401/500/503).

Transport: no constructor on this pin — the compile-time mirror assertion
in the pin file proves the profile-read-scoped re-declaration matches the
staying handler-facing port. Concrete-double-only proof is rejected.

Enforcement (all in go/internal/query):

- `profile_read_admission_contract_test.go` — frozen method-set test plus
  version pin (`TestProfileReadAdmissionContractIsVersioned`), witness
  (`profileReadAdmissionWitness` plus satisfier assertion), seal
  (`TestProfileReadAdmissionWireShapesAreSealed` — exactly 1 read), and
  the seeded-drift RED half
  (`TestProfileReadAdmissionSeededDriftIsDetected` — a planted widening
  must not match the frozen set).

## Satellite consumption

Depend on the shape, not a constructor: serve the profile leg through the
pin, backed by the same local-identity profile store the leg runs against
today. A satellite PR consuming this pin for any other local-identity leg
is invalid without arbiter review.

## W8a disclosed-absence posture

`GET /api/v0/auth/profile` is natively mounted in-tree and its OpenAPI
fragment stays core-side (oracle `paths/auth/routes.go`, codes above) —
disclosed here, not silently dropped. No `contracts/upstream/*` stub covers
this leg: the wire-shape generator snapshots forwarded relay spellings
only, and no Profile leg has one.

## Explicitly OUT

- The token-list, user-ID resolution, and TOTP enrollment legs ride the
  sibling `LocalIdentityAdmission` pin, never this one.
- Grant checks and fixed failure messages are handler behavior, not port
  shape.
- No constructor surface: the satellite satisfies this port with its own
  wiring; this pin carries the read shape only.

## Rollback

Revert this pin. The seam is unwired — no handler, route, or caller depends
on it yet — so rollback moves no traffic and changes no data.
