# CodeDivergenceReadAdmission v0 (core pin record)

Refs eshu-hq/eshu#9669. Wave-4 Code tier-b core pin, per the arbiter
step-2 scope ruling (decisions 2, 6, 7). Minted core-side; the satellite
records this pin in a follow-up. A satellite PR consuming an unminted
pin is invalid. Arbiter review of this pin precedes satellite
termination of the divergence findings leg.

## Shape

Version: `CodeDivergenceReadAdmissionContractVersion = "v0"`
(go/internal/query/codequery/code_divergence_read_admission.go).

Exactly 6 re-declared (never embedded) methods, sealed — no
single-row, search, list, ledger, or snapshot surface rides along:

| # | Method | Mirrors | Serves |
|---|--------|---------|--------|
| 1 | `DivergenceGroupStats` | staying `divergenceStore` port | `POST /api/v0/code/divergence/findings` group stats |
| 2 | `DivergenceMembers` | staying `divergenceStore` port | findings member hydration |
| 3 | `DivergenceMembersByEntityID` | staying `divergenceStore` port | findings entity-ID hydration |
| 4 | `DriftedFindingStats` | staying `divergenceStore` port | findings drifted stats |
| 5 | `DriftedFindingRows` | staying `divergenceStore` port | findings drifted rows |
| 6 | `Run` | `querycontract.GraphQuery` | findings bounded graph traversal |

Route oracle codes (oracle `8a78eb3f48`,
`paths/code/divergence.go`): `POST /api/v0/code/divergence/findings`
(200/400/403/500/503/504).

Transport: no `WithReadStore` constructor on this pin — the findings
seam spans the divergence store plus the bounded graph traversal and
minting one constructor would pick a wiring the satellite never runs.
The compile-time mirror assertion in the pin file (against the staying
`divergenceStore` and `GraphQuery` shapes) proves the pair the route
runs against today satisfies the pin. Concrete-double-only proof is
rejected.

Enforcement (all in go/internal/query/codequery):

- `code_divergence_read_admission_contract_test.go` — frozen
  method-set test plus version pin
  (`TestCodeDivergenceReadAdmissionContractIsVersioned`), witness
  (`codeDivergenceReadAdmissionWitness` + satisfier assertion) and its
  re-signed twin
  (`TestCodeDivergenceReadAdmissionRejectsResignedMethod` — a
  re-signed `DriftedFindingRows` must break the pin), seal
  (`TestCodeDivergenceReadAdmissionWireShapesAreSealed` — exactly 6
  methods, no `RunSingle`/search/list/ledger/snapshot surface), and
  the seeded-drift RED half
  (`TestCodeDivergenceReadAdmissionSeededDriftIsDetected` — the
  planted `RunSingle` widening must not match the frozen set).

## Satellite consumption

Depend on the shape, not a constructor: serve the findings leg through
the existing divergence store plus the bounded graph reader, the same
pair the route runs against today. A satellite PR consuming this pin
for any other backend pair is invalid without arbiter review.

## W8a disclosed-absence posture

`POST /api/v0/code/divergence/findings` is natively mounted in-tree
(`CodeHandler.Mount`, go/internal/query/codequery/handler.go) and its
OpenAPI fragment stays core-side (oracle `paths/code/divergence.go`,
200/400/403/500/503/504) — disclosed here, not silently dropped. No
`contracts/upstream/*` stub covers this leg: the wire-shape generator
snapshots forwarded relay spellings only, and no divergence leg has
one.

## Explicitly OUT (not widened by this pin)

- `POST /api/v0/code/divergence/investigate`
  (200/400/403/404/500/503/504) and `POST
  /api/v0/code/divergence/report` (200/400/403/500/503) — these legs
  admit through no versioned pin yet; they keep their own standing
  until a deliberate pin covers them, and a satellite PR must not cite
  this pin as their coverage.
- `RunSingle` — the single-row graph read; the findings lane never
  issues one. Proven at the seal, not just in the frozen set.
- Name, exact-name, component, and language/type searches — the Code
  search and relationships pins cover them.
- Grant checks, the kind whitelist, fixed failure messages, and
  `QueryProfile` (handler behavior, not port shape).
- Ledger / stored-summary / snapshot surface (none on this family;
  sealed out regardless).

## Rollback

Revert this pin. The seam is unwired — no handler, route, or caller
depends on it yet — so rollback moves no traffic and changes no data.
