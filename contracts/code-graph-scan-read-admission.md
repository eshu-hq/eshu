# CodeGraphScanReadAdmission v0 (core pin record)

Refs eshu-hq/eshu#9669. Wave-4 Code tier-b core pin, per the arbiter
step-2 scope ruling (decisions 2, 6, 7). Minted core-side; the satellite
records this pin in a follow-up. A satellite PR consuming an unminted
pin is invalid. Arbiter review of this pin precedes satellite
termination of the W6b graph-scan legs.

## Shape

Version: `CodeGraphScanReadAdmissionContractVersion = "v0"`
(go/internal/query/codequery/code_graph_scan_read_admission.go).

Exactly 2 re-declared (never embedded) methods, sealed — `RunSingle`
is OUT by construction, and no search, file, catalog, ledger, or
snapshot surface rides along:

| # | Method | Mirrors | Serves |
|---|--------|---------|--------|
| 1 | `Run` | `querycontract.GraphQuery` | `POST /api/v0/code/complexity` (by-name, by-ID, ranked-list builders), `POST /api/v0/code/quality/inspect`, `POST /api/v0/code/imports/investigate` (imports leaf dispatch), `POST /api/v0/code/bundles` (anchor + version counts) |
| 2 | `ListRepoEntitiesByIDs` | staying `ContentStore` port | by-ID entity hydration behind the complexity by-ID branch |

Route oracle codes (oracle `8a78eb3f48`): `POST
/api/v0/code/complexity` (`paths/code/routes.go`:
200/400/403/404/409/500/503/504), `POST /api/v0/code/quality/inspect`
(`paths/code/quality.go`: 200/400/403/500/503/504), `POST
/api/v0/code/imports/investigate` (`paths/code/symbols.go`:
200/400/403/422/500/503/504), `POST /api/v0/code/bundles`
(`paths/code/graph.go`: 200/400/403/500/503/504).

Transport: no `WithReadStore` constructor on this pin — the W6b seam
spans the bounded graph traversal plus the by-ID hydration read and
minting one constructor would pick a wiring the satellite never runs.
The compile-time mirror assertion in the pin file proves the pair the
legs run against today satisfies the pin.

Evidence (not enforcement): the wire-shapes proof through the real
`Mount` plus the store fail-closed suite
(`code_graph_scan_read_admission_wire_shapes_test.go` —
`TestCodeGraphScanReadAdmissionWireShapesAreSatelliteConsumable`).

Enforcement (all in go/internal/query/codequery):

- `code_graph_scan_read_admission_contract_test.go` — frozen
  method-set test plus version pin
  (`TestCodeGraphScanReadAdmissionContractIsVersioned`), witness
  (`codeGraphScanReadAdmissionWitness` + satisfier assertion) and its
  re-signed twin
  (`TestCodeGraphScanReadAdmissionRejectsResignedMethod`), seal
  (`TestCodeGraphScanReadAdmissionWireShapesAreSealed` — exactly 2
  methods, no `RunSingle`/search/file/catalog/ledger/summary
  surface), and the seeded-drift RED half
  (`TestCodeGraphScanReadAdmissionSeededDriftIsDetected` — the planted
  `RunSingle` widening must not match the frozen set).

## Satellite consumption

Depend on the shape, not a constructor: serve the four legs through
the existing bounded graph reader plus the guarded content store (for
the by-ID hydration branch), the same pair the legs run against today.
A satellite PR consuming this pin for any other backend pair is invalid
without arbiter review.

## W8a disclosed-absence posture

`POST /api/v0/code/complexity`, `POST /api/v0/code/quality/inspect`,
`POST /api/v0/code/imports/investigate`, and `POST
/api/v0/code/bundles` are natively mounted in-tree
(`CodeHandler.Mount`, go/internal/query/codequery/handler.go) and
their OpenAPI fragments stay core-side (oracle `paths/code/routes.go`,
`paths/code/quality.go`, `paths/code/symbols.go`, and
`paths/code/graph.go`, codes above) — disclosed here, not silently
dropped. No `contracts/upstream/*` stub covers these legs: the
wire-shape generator snapshots forwarded relay spellings only
(`PATCH /api/v0/code/bundles`, the R1 rider, rides the generic relay
and is pinned separately in contracts/step2-frozen-scope.md plus
`contracts/upstream/code-read-wire-shapes.txt`), and no W6b leg itself
has a forwarded spelling.

## Explicitly OUT (not widened by this pin)

- `RunSingle` — the single-row graph read; no W6b route issues one.
  Proven at the seal, not just in the frozen set.
- cypher/visualize (caller-supplied programs, own pin),
  structure/inventory, language-query, topics/investigate,
  secrets/investigate — sibling-family reads with their own contracts.
- Every other `ContentStore` method, including `GetEntityContent`
  (the by-ID enrichment other routes use; the scan lane hydrates
  through `ListRepoEntitiesByIDs`): adopted only by deliberate
  re-declaration plus a frozen-list update.
- Grant checks, the grant-bound builders, the repository selector,
  fixed failure messages, and `QueryProfile` (handler behavior, not
  port shape).

## Rollback

Revert this pin. The seam is unwired — no handler, route, or caller
depends on it yet — so rollback moves no traffic and changes no data.
