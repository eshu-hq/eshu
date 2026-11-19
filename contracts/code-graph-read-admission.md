# CodeGraphReadAdmission v0 (core pin record)

Refs eshu-hq/eshu#9669. Wave-4 Code tier-b core pin, per the arbiter
step-2 scope ruling (decisions 2, 6, 7). Minted core-side; the satellite
records this pin in a follow-up. A satellite PR consuming an unminted
pin is invalid. Arbiter review of this pin precedes satellite
termination of the G-a graph legs.

## Shape

Version: `CodeGraphReadAdmissionContractVersion = "v0"`
(go/internal/query/codequery/code_graph_read_admission.go).

Exactly 3 re-declared (never embedded) methods, sealed — no list,
catalog, language, component, ledger, or snapshot surface rides along:

| # | Method | Mirrors | Serves |
|---|--------|---------|--------|
| 1 | `Run` | `querycontract.GraphQuery` | `POST /api/v0/code/call-chain`, `POST /api/v0/code/call-chain/compare`, `POST /api/v0/code/routes/callers` bounded traversals |
| 2 | `RunSingle` | `querycontract.GraphQuery` | single-row graph reads on the same three legs |
| 3 | `SearchEntitiesByName` | staying `ContentStore` port | entity-name resolution on the same three legs |

Route oracle codes (oracle `8a78eb3f48`): `POST
/api/v0/code/call-chain` (`paths/code/routes.go`:
200/400/403/500/503/504), `POST /api/v0/code/call-chain/compare`
(`paths/code/routes.go`: 200/400/403/500/503/504), `POST
/api/v0/code/routes/callers` (`paths/code/route_to_caller.go`:
200/400/403/404/409/500/501/503/504).

Transport: no `WithReadStore` constructor on this pin — the G-a seam
spans the bounded graph reader plus one content-store read and minting
one constructor would pick a wiring the satellite never runs. The
compile-time mirror assertion in the pin file proves the pair the legs
run against today satisfies the pin. Concrete-double-only proof is
rejected.

Enforcement (all in go/internal/query/codequery):

- `code_graph_read_admission_contract_test.go` — frozen method-set
  test plus version pin
  (`TestCodeGraphReadAdmissionContractIsVersioned`), witness
  (`codeGraphReadAdmissionWitness` + satisfier assertion) and its
  re-signed twin
  (`TestCodeGraphReadAdmissionRejectsResignedMethod`), seal
  (`TestCodeGraphReadAdmissionWireShapesAreSealed` — exactly 3
  methods, no list/catalog/language/component/ledger/snapshot
  surface), and the seeded-drift RED half
  (`TestCodeGraphReadAdmissionSeededDriftIsDetected` — the planted
  `ListRepositories` widening must not match the frozen set).

## Satellite consumption

Depend on the shape, not a constructor: serve the three legs through
the existing bounded graph reader plus the guarded content store, the
same pair the legs run against today. A satellite PR consuming this
pin for any other backend pair is invalid without arbiter review.

## W8a disclosed-absence posture

`POST /api/v0/code/call-chain`, `POST
/api/v0/code/call-chain/compare`, and `POST /api/v0/code/routes/callers`
are natively mounted in-tree (`CodeHandler.Mount`,
go/internal/query/codequery/handler.go) and their OpenAPI fragments
stay core-side (oracle `paths/code/routes.go` and
`paths/code/route_to_caller.go`, codes above) — disclosed here, not
silently dropped. No `contracts/upstream/*` stub covers these legs: the
wire-shape generator snapshots forwarded relay spellings only, and no
G-a leg has one.

## Explicitly OUT — `POST /api/v0/code/call-graph/metrics` stays codeGraphWave-owned

`POST /api/v0/code/call-graph/metrics` (oracle
`paths/code/symbols.go`: 200/400/403/422/500/503/504) is NOT absorbed by
this Wave-4 record. It stays under codeGraphWave ownership for its own
termination lane; Wave 4 (this record) must never absorb it. The Go pin
re-declares the same three reads the metrics leg exercises, but the
route itself is carved out of Wave-4 scope — a satellite PR citing this
record as metrics-leg coverage is invalid.

Also OUT (each keeps its own standing until a deliberate pin covers
it):

- `POST /api/v0/code/relationships/story` (G-b, sibling pin —
  see contracts/code-relationships-read-admission.md).
- complexity/quality, bundles, cypher/visualize,
  structure/inventory, language-query, and `GET
  /api/v0/graph/entities` (W8b): sibling-family surfaces, never
  adopted here.
- Every other `ContentStore` method: this pin adopts
  `SearchEntitiesByName` only; a new shared-port method is adopted
  only by deliberate re-declaration plus a frozen-list update.
- Grant checks, the unsupported-language short-circuit, fixed failure
  messages, and `QueryProfile` (handler behavior, not port shape).

## Rollback

Revert this pin. The seam is unwired — no handler, route, or caller
depends on it yet — so rollback moves no traffic and changes no data.
