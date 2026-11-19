# DependencyReadAdmission v0 (core pin record)

Refs eshu-hq/eshu#9669. Wave-5 Dependencies tier-b core pin, per the
arbiter step-2 scope ruling (decisions 2, 6, 7). Minted core-side;
the satellite records this pin in a follow-up. A satellite PR
consuming an unminted pin is invalid. Arbiter review of this pin
precedes satellite termination of the dependency leg.

## Shape

Version: `DependencyReadAdmissionContractVersion = "v0"`
(go/internal/query/dependency/read_admission.go).

Exactly 1 re-declared (never embedded) method, sealed — built by one
statement builder for the forward and reverse legs:

| # | Method | Mirrors | Serves |
|---|--------|---------|--------|
| 1 | `Run` | `querycontract.GraphQuery` | `GET /api/v0/dependencies` bounded graph read |

Route oracle codes (oracle `8a78eb3f48`): `GET /api/v0/dependencies`
(`paths/repository/dependencies.go`: 200/400/501/503/504).

Transport: no witness, no constructor, selector, or `ReadStore`
surface on this pin — the mirror assertion is the standing proof: the
compile-time assertion keeps the re-declaration honest in one
direction (removal, rename, or re-signing of `Run` on the staying
handler-facing port fails compilation here), and a new method on that
source port does not widen this pin. Duplicate re-declaration of
`Run` across the W8b Run-only pins is correct, not waste: each pin
freezes its own family shape.

Enforcement (all in go/internal/query/dependency):

- `read_admission_contract_test.go` — frozen method-set test plus
  version pin (`TestDependencyReadAdmissionContractIsVersioned`),
  the parameterized `dependencyReadAdmissionMethodSet` reflection
  helper behind the seal
  (`TestDependencyReadAdmissionWireShapesAreSealed` — exactly 1
  method), and the seeded-drift RED half
  (`TestDependencyReadAdmissionSeededDriftIsDetected` — the planted
  `seededDependencyDrift` widening must not match the frozen set).
  No witness rides along: the mirror assertion above is the recorded
  standing instead.

## Satellite consumption

Depend on the shape, not a constructor: serve the leg through the
existing graph reader, the same read the leg runs against today. This
is the W8b-graph1 leaf, per the arbiter SUPPLEMENTAL-PIN-OWED ruling
on eshu-hq/eshu-api#101. Note: this route is the standing relay-leg
benchmark leaf in satellite latency suites; the leaf re-points those
benchmarks, never deletes them. A satellite PR consuming this pin for
any other backend is invalid without arbiter review.

## W8a disclosed-absence posture

`GET /api/v0/dependencies` is natively mounted in-tree and its
OpenAPI fragment stays core-side (oracle
`paths/repository/dependencies.go`, codes above) — disclosed here,
not silently dropped. No `contracts/upstream/*` stub covers this leg:
the wire-shape generator snapshots forwarded relay spellings only,
and no Dependencies leg has one.

## Explicitly OUT

Also OUT (each keeps its own standing until a deliberate pin covers
it):

- `GraphQuery.RunSingle` stays unpinned by construction: the
  dependency lane never issues a single-row graph read.
- The grant checks, the fixed failure messages, and `QueryProfile`
  (handler behavior, not port shape).

## Rollback

Revert this pin. The seam is unwired — no handler, route, or caller
depends on it yet — so rollback moves no traffic and changes no data.
