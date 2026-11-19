# CodeownersOwnershipReadAdmission v0 (core pin record)

Refs eshu-hq/eshu#9669. Wave-5 CodeownersOwnership tier-b core pin,
per the arbiter step-2 scope ruling (decisions 2, 6, 7). Minted
core-side; the satellite records this pin in a follow-up. A satellite
PR consuming an unminted pin is invalid. Arbiter review of this pin
precedes satellite termination of the ownership leg.

## Shape

Version: `CodeownersOwnershipReadAdmissionContractVersion = "v0"`
(go/internal/query/codeowners/ownership_read_admission.go).

Exactly 3 re-declared (never embedded) methods, sealed — the graph
`Run` x1 or x3 cursor branches, the `RunSingle` last-match on a
manifest miss, and the catalog leg through
`ListServiceCatalogCorrelations` (runs first on the
manifest-precedence path):

| # | Method | Mirrors | Serves |
|---|--------|---------|--------|
| 1 | `Run` | `querycontract.GraphQuery` | `GET /api/v0/codeowners/ownership` cursor-branch graph reads |
| 2 | `RunSingle` | `querycontract.GraphQuery` | same leg, last-match on a manifest miss |
| 3 | `ListServiceCatalogCorrelations` | `querycontract.ServiceCatalogCorrelationStore` | same leg, manifest-precedence catalog read |

Route oracle codes (oracle `8a78eb3f48`): `GET
/api/v0/codeowners/ownership` (`paths/code/owners.go`:
200/400/403/501/503/504).

Transport: no constructor, selector, or `ReadStore` surface on this
pin. The `codeownersOwnershipReadAdmissionMirror` keeps both staying
handler-facing sources honest in one assertion (graph plus catalog
correlation ports), and the satisfier assertion (the concrete
`PostgresServiceCatalogCorrelationStore` built by the former in-tree
wiring) proves the catalog leg serves with the minimal shape. The
ServiceCatalogCorrelation pin separately re-declares the same
signature for its own route; neither pin cites the other.
Concrete-double-only proof is rejected.

Enforcement (all in go/internal/query/codeowners):

- `ownership_read_admission_contract_test.go` — frozen method-set
  test plus version pin
  (`TestCodeownersOwnershipReadAdmissionContractIsVersioned`), seal
  (`TestCodeownersOwnershipReadAdmissionWireShapesAreSealed` —
  exactly 3 methods), serve-the-route
  (`TestCodeownersOwnershipReadAdmissionWireShapesServeTheRoute` —
  the pin-typed `codeownersOwnershipAdmissionCannedStore`, including
  the `RunSingle` last-match leg on a manifest miss), and the
  seeded-drift RED half
  (`TestCodeownersOwnershipReadAdmissionSeededDriftIsDetected`).

## Satellite consumption

Depend on the shape, not a constructor: serve the leg through the
existing graph reader plus the Postgres catalog store, the same pair
the leg runs against today. This is the W8b-wide3 leaf, per the
arbiter SUPPLEMENTAL-PIN-OWED ruling on eshu-hq/eshu-api#101. A
satellite PR consuming this pin for any other backend pair is invalid
without arbiter review.

## W8a disclosed-absence posture

`GET /api/v0/codeowners/ownership` is natively mounted in-tree and
its OpenAPI fragment stays core-side (oracle
`paths/code/owners.go`, codes above) — disclosed here, not silently
dropped. No `contracts/upstream/*` stub covers this leg: the
wire-shape generator snapshots forwarded relay spellings only, and no
CodeownersOwnership leg has one.

## Explicitly OUT

Also OUT (each keeps its own standing until a deliberate pin covers
it):

- Scoped cross-repo empty page (handler behavior, not port shape).
- The grant checks, the fixed failure messages, and `QueryProfile`
  (handler behavior, not port shape).

## Rollback

Revert this pin. The seam is unwired — no handler, route, or caller
depends on it yet — so rollback moves no traffic and changes no data.
