# CodeFlowReadAdmission v0 (core pin record)

Refs eshu-hq/eshu#9669. W6a Code-flow 4 ONLY, per the arbiter W-wave
ruling (APPROVED-WITH-CONDITIONS). Minted core-side; the satellite
records this pin in a follow-up. A satellite PR consuming an unminted
pin is invalid. Arbiter review of this pin precedes satellite
termination of the Code-flow 4 legs. W6b-W6f are NOT approved and are
not pre-minted here.

## Shape

Version: `CodeFlowReadAdmissionContractVersion = "v0"`
(go/internal/query/codequery/code_flow_read_admission.go).

Exactly 1 re-declared (never embedded) method, sealed — no search,
no list, no ledger/summary/snapshot surface:

| # | Method | Mirrors (staying handler port) | Serves |
|---|--------|-------------------------------|--------|
| 1 | `ListCodeFlow(context.Context, codemodel.CodeFlowFilter) (codemodel.CodeFlowReadModel, error)` | `CodeFlowStore` in go/internal/query/codequery/flow.go | `POST /api/v0/code/flow/cfg-summary`, `POST /api/v0/code/flow/pdg-summary`, `POST /api/v0/code/flow/reaching-def`, `POST /api/v0/code/flow/taint-path` |

Transport: `NewCodeFlowReadAdmissionWithReadStore(db.Queryer)`
delegates to the existing `codemodel.NewPostgresCodeFlowStoreWithReadStore`
(no parallel wiring); the guarded-Queryer fail-closed proof plus the
full wire-object byte mirror against the direct constructor covers
the satellite path end to end. Concrete-double-only proof is
rejected.

Evidence (not enforcement): the pin-satisfying route double is
`codeFlowAdmissionRouteDouble`
(go/internal/query/codequery/code_flow_read_admission_wire_shapes_test.go);
the transport mirror serves canned function and taint rows through
`codeFlowAdmissionCannedQueryer` over the same file.

Enforcement (all in go/internal/query/codequery):

- `code_flow_read_admission_contract_test.go` — frozen method-set test
  plus version pin (`TestCodeFlowReadAdmissionContractIsVersioned`).
- `code_flow_read_admission_wire_shapes_test.go` — wire-shapes proof:
  `NewCodeFlowReadAdmissionWithReadStore(db.Queryer)` serves the
  pinned read over nothing wider than the guarded `Queryer`
  (fail-closed on unavailable reader), and the full wire-object byte
  mirror against the direct `codemodel` constructor over the same
  canned rows (function row and taint row), with want/got unified
  through one projection
  (`TestCodeFlowReadAdmissionTransportIsSatelliteConsumable`).
- `code_flow_read_admission_wire_shapes_test.go` — route proof through
  the real `Mount`: the pin-satisfying double serves all four routes
  with byte-exact `ListCodeFlow` projections (per-route projection
  key and fact label asserted), the store-error fail-closed suite
  answers the existing fixed message, and a nil store preserves the
  `flow.go:104` unavailable verdict
  (`TestCodeFlowReadAdmissionRoutesArePinSatisfiable`,
  `TestCodeFlowReadAdmissionRoutesFailClosed`).
- Seal (`TestCodeFlowReadAdmissionContractIsVersioned`) — exactly
  1 method, no ledger/summary/snapshot surface.
- Compile-time mirror assertion in the pin file:
  `var _ CodeFlowReadAdmission = CodeFlowStore(nil)` — asserted
  against the staying handler-facing port (not the concrete Postgres
  store), so the pin stays identical to what the handlers actually
  call while the concrete store keeps its own standing.

## Satellite consumption

Build the satisfier with
`codequery.NewCodeFlowReadAdmissionWithReadStore(reader)` where
`reader` is the request-scoped `db.Queryer`.

## Explicitly OUT (not widened by this pin)

- Grant checks, repository selector, unsupported-language
  short-circuit, fixed failure messages (`"code-flow read failed"`,
  `"code-flow store is unavailable"`), and QueryProfile (handler
  behavior, not port shape).
- Search legs (no search surface exists on this family; sealed out
  regardless).
- List legs (no list surface exists on this family; sealed out
  regardless).
- Ledger / stored-summary / snapshot surface (none on this family;
  sealed out regardless).
