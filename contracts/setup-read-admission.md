# SetupReadAdmission v0 (W5 pin record)

Refs eshu-hq/eshu#9669. Wave-5 Setup tier-b core pin, per the arbiter
step-2 scope ruling (decisions 2, 6, 7). Minted core-side; the satellite
records this pin in a follow-up. A satellite PR consuming an unminted pin is
invalid. Arbiter review of this pin precedes satellite termination of the
setup-state legs.

## Shape

Version: `SetupReadAdmissionContractVersion = "v0"`
(go/internal/query/setup_read_admission.go).

Exactly 1 read, re-declared in this setup-read-scoped shape instead of
embedding or aliasing `SetupStore` — the claim, admin, MFA, and
credential-verification consumers keep their own standing:

| # | Method | Mirrors | Serves |
|---|--------|---------|--------|
| 1 | `SetupNeeded` | `SetupStore` (as exercised through the setup-state leg) | `GET /api/v0/auth/setup-state` |

Route oracle codes (oracle `8a78eb3f48`): `GET /api/v0/auth/setup-state`
(`paths/auth/setup.go`: 200/503).

Transport: no constructor on this pin — the compile-time mirror assertion
in the pin file proves the setup-read-scoped re-declaration matches the
staying handler-facing port. Concrete-double-only proof is rejected.

Enforcement (all in go/internal/query):

- `setup_read_admission_contract_test.go` — frozen method-set test plus
  version pin (`TestSetupReadAdmissionContractIsVersioned`), witness
  (`setupReadAdmissionWitness` plus satisfier assertion), seal
  (`TestSetupReadAdmissionWireShapesAreSealed` — exactly 1 read), and the
  seeded-drift RED half (`TestSetupReadAdmissionSeededDriftIsDetected` —
  a planted widening must not match the frozen set).

## Satellite consumption

Depend on the shape, not a constructor: serve the setup-state leg through
the pin, backed by the same setup store the leg runs against today. A
satellite PR consuming this pin for any setup mutation or any other family
is invalid without arbiter review.

## W8a disclosed-absence posture

`GET /api/v0/auth/setup-state` is natively mounted in-tree and its OpenAPI
fragment stays core-side (oracle `paths/auth/setup.go`, codes above) —
disclosed here, not silently dropped. No `contracts/upstream/*` stub covers
this leg: the wire-shape generator snapshots forwarded relay spellings
only, and no Setup leg has one.

## Explicitly OUT — the three setup mutations ride the sibling SetupAdmission writer pin

The setup claim, admin, and MFA mutations ride the sibling `SetupAdmission`
writer pin in `setup_types.go`, never this one.
[contracts/setup-writer-admission.md](setup-writer-admission.md) already
covers the writer; a satellite PR citing this read record as mutation-leg
coverage is invalid.

## Rollback

Revert this pin. The seam is unwired — no handler, route, or caller depends
on it yet — so rollback moves no traffic and changes no data.
