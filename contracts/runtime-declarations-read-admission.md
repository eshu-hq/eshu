# RuntimeDeclarationReadAdmission v0 (core pin record)

Refs eshu-hq/eshu#9669. Wave-5 RuntimeDeclarations tier-b core pin,
per the arbiter step-2 scope ruling (decisions 2, 6, 7). Minted
core-side; the satellite records this pin in a follow-up. A satellite
PR consuming an unminted pin is invalid. Arbiter review of this pin
precedes satellite termination of the declaration leg.

## Shape

Version: `RuntimeDeclarationReadAdmissionContractVersion = "v0"`
(go/internal/query/runtime/declaration/read_admission.go).

Exactly 1 re-declared (never embedded) method, sealed — issued always
for the page, conditionally for the excluded count, and degradably
for names:

| # | Method | Mirrors | Serves |
|---|--------|---------|--------|
| 1 | `Run` | `querycontract.GraphQuery` | `POST /api/v0/runtime-declarations/query` bounded graph read |

Route oracle codes (oracle `8a78eb3f48`): `POST
/api/v0/runtime-declarations/query`
(`paths/repository/runtime_declarations.go`:
200/400/403/501/503/504).

Transport: no constructor, selector, or `ReadStore` surface on this
pin — the satellite satisfies it with its own wiring. The
compile-time mirror assertion keeps the re-declaration honest in one
direction: removal, rename, or re-signing of `Run` on the staying
handler-facing port fails compilation here. Duplicate re-declaration
of `Run` across the W8b Run-only pins is correct, not waste: each pin
freezes its own family shape. Concrete-double-only proof is rejected.

Enforcement (all in go/internal/query/runtime/declaration):

- `read_admission_contract_test.go` — frozen method-set test plus
  version pin
  (`TestRuntimeDeclarationReadAdmissionContractIsVersioned`), seal
  (`TestRuntimeDeclarationReadAdmissionWireShapesAreSealed` —
  exactly 1 method), and the seeded-drift RED half
  (`TestRuntimeDeclarationReadAdmissionSeededDriftIsDetected` — the
  planted `seededRuntimeDeclarationDrift` widening must not match the
  frozen set).

## Satellite consumption

Depend on the shape, not a constructor: serve the leg through the
existing graph reader, the same read the leg runs against today. This
is the W8b-graph2 leaf, per the arbiter SUPPLEMENTAL-PIN-OWED ruling
on eshu-hq/eshu-api#101. A satellite PR consuming this pin for any
other backend is invalid without arbiter review.

## W8a disclosed-absence posture

`POST /api/v0/runtime-declarations/query` is natively mounted in-tree
and its OpenAPI fragment stays core-side (oracle
`paths/repository/runtime_declarations.go`, codes above) — disclosed
here, not silently dropped. No `contracts/upstream/*` stub covers this
leg: the wire-shape generator snapshots forwarded relay spellings
only, and no RuntimeDeclarations leg has one.

## Explicitly OUT

Also OUT (each keeps its own standing until a deliberate pin covers
it):

- `GraphQuery.RunSingle` stays unpinned by construction: the
  declaration lane never issues a single-row graph read.
- `LoadSection` stays OUT: it serves the repository-context section,
  not this route.
- The grant checks, the fixed failure messages, and `QueryProfile`
  (handler behavior, not port shape).

## Rollback

Revert this pin. The seam is unwired — no handler, route, or caller
depends on it yet — so rollback moves no traffic and changes no data.
