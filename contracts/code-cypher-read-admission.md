# CodeCypherReadAdmission v0 (core pin record)

Refs eshu-hq/eshu#9669. Wave-4 Code tier-b core pin, per the arbiter
step-2 scope ruling (decisions 2, 6, 7). Minted core-side; the satellite
records this pin in a follow-up. A satellite PR consuming an unminted
pin is invalid. Arbiter review of this pin precedes satellite
termination of the cypher and visualize legs.

## Shape

Version: `CodeCypherReadAdmissionContractVersion = "v0"`
(go/internal/query/codequery/code_cypher_read_admission.go).

Exactly 1 re-declared (never embedded) method, sealed — no
single-row, search, list, point, investigation, count, topic, secret,
or snapshot surface rides along:

| # | Method | Mirrors | Serves |
|---|--------|---------|--------|
| 1 | `Run` | `querycontract.GraphQuery` | `POST /api/v0/code/cypher` bounded read-only query, `POST /api/v0/code/visualize` bounded query plus packet projection |

Route oracle codes (oracle `8a78eb3f48`, `paths/code/graph.go`):
`POST /api/v0/code/cypher` (200/400/500/501/503/504), `POST
/api/v0/code/visualize` (200/400/500/501/503/504). Both legs are
shared-key/all-scope only (no tenant grant to intersect: the query text
is caller-supplied and unbounded); scoped and browser-session tokens
are rejected before the handler runs.

Transport: no `WithReadStore` constructor on this pin — the cypher seam
is the single bounded graph traversal and minting a store constructor
would pick a wiring the satellite never runs. The compile-time mirror
assertion in the pin file proves the reader the legs run against today
satisfies the pin.

Evidence (not enforcement): the wire-shapes proof through the real
`Mount` (`code_cypher_read_admission_wire_shapes_test.go` —
`TestCodeCypherReadAdmissionWireShapesAreSatelliteConsumable`).

Enforcement (all in go/internal/query/codequery):

- `code_cypher_read_admission_contract_test.go` — frozen method-set
  test plus version pin
  (`TestCodeCypherReadAdmissionContractIsVersioned`), witness
  (`codeCypherReadAdmissionWitness` + satisfier assertion) and its
  re-signed twin
  (`TestCodeCypherReadAdmissionRejectsResignedMethod`), seal
  (`TestCodeCypherReadAdmissionWireShapesAreSealed` — exactly 1
  method, no single-row/search/list/point/investigation/secret
  surface), and the seeded-drift RED half
  (`TestCodeCypherReadAdmissionSeededDriftIsDetected` — the planted
  `RunSingle` widening must not match the frozen set).

## Satellite consumption

Depend on the shape, not a constructor: serve both legs through the
existing bounded graph reader with the same read-only safety path
(mutation-keyword rejection, injected terminal LIMIT, timeout-bound
`AccessModeRead` session, row-window cap) the legs run against today. A
satellite PR consuming this pin for any other backend is invalid
without arbiter review. No Cypher text, index, or planner change rides
this record: the pin freezes only the read shape, and the safety path
stays handler behavior.

## W8a disclosed-absence posture

`POST /api/v0/code/cypher` and `POST /api/v0/code/visualize` are
natively mounted in-tree (`CodeHandler.Mount`,
go/internal/query/codequery/handler.go) and their OpenAPI fragments
stay core-side (oracle `paths/code/graph.go`, codes above) — disclosed
here, not silently dropped. No `contracts/upstream/*` stub covers
these legs beyond the frozen-scope R1 rider record: the wire-shape
generator snapshots forwarded relay spellings only, and neither leg
itself has one.

## Explicitly OUT (not widened by this pin)

- `RunSingle` — the single-row graph read; the cypher lane never
  issues one. Proven at the seal, not just in the frozen set.
- Purpose-built code, service, and impact routes — the cypher endpoint
  is diagnostics-only by design; callers with a shaped need stay on
  their own pins.
- The mutation-keyword rejection, LIMIT injection, timeout, and row
  cap (handler safety behavior, not port shape).
- Grant checks beyond the shared-key gate and `QueryProfile` (handler
  behavior, not port shape).

## Rollback

Revert this pin. The seam is unwired — no handler, route, or caller
depends on it yet — so rollback moves no traffic and changes no data.
