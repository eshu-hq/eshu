# ContentPointReadAdmission v0 (core pin record)

Refs eshu-hq/eshu#9669. D13 Content-first Wave-2 core pin, per the
arbiter Wave-2 ruling (APPROVED-WITH-CONDITIONS). Minted core-side;
the satellite records this pin in a follow-up. A satellite PR
consuming an unminted pin is invalid. Arbiter review of this pin
precedes satellite termination of the content point legs.

## Shape

Version: `ContentPointReadAdmissionContractVersion = "v0"`
(go/internal/query/content_read_admission.go).

Exactly 3 re-declared (never embedded) methods, sealed — no search,
no list, no ledger/summary/snapshot surface:

| # | Method | Mirrors (*ContentReader) | Serves |
|---|--------|--------------------------|--------|
| 1 | `GetFileContent(context.Context, string, string) (*querycontract.FileContent, error)` | go/internal/query/content_reader.go | `POST /api/v0/content/files/read` |
| 2 | `GetFileLines(context.Context, string, string, int, int) (*querycontract.FileContent, error)` | go/internal/query/content_reader.go | `POST /api/v0/content/files/lines` |
| 3 | `GetEntityContent(context.Context, string) (*querycontract.EntityContent, error)` | go/internal/query/content_reader_entity.go | `POST /api/v0/content/entities/read` |

Transport: `NewContentPointReadAdmissionWithReadStore(db.ReadStore)`
delegates to the existing `NewContentReaderWithReadStore` (no
parallel wiring); the guarded-ReadStore fail-closed proof plus the
full wire-object byte mirror against `*ContentReader` covers the
satellite path end to end. Concrete-double-only proof is rejected.

Evidence (not enforcement): the shared handler double is
testutil/content.FakePortContentStore, blob
`5c836cce6245ac9c470a837c986fcde96e40946d`
(go/internal/query/testutil/content/port_store.go, last touched by
`030c434afda78a7c0d5ec6dbb97a877237b4837c`).

Enforcement (all in go/internal/query):

- `content_read_admission_contract_test.go` — frozen method-set test
  plus version pin (`TestContentPointReadAdmissionContractIsVersioned`).
- `content_read_admission_wire_shapes_test.go` — wire-shapes proof:
  `NewContentPointReadAdmissionWithReadStore(db.ReadStore)` serves
  all three reads over nothing wider than the guarded `ReadStore`
  (fail-closed on unavailable reader), and the full wire-object byte
  mirror against `*ContentReader` over the same store (file found and
  missing, entity found and missing, and GetFileLines slicing edges:
  full range, sub-range, start clamp, end-zero, end clamp, and
  start-past-end) with want/got unified through one projection
  (`TestContentPointReadAdmissionWireShapesAreSatelliteConsumable`).
- Seal (`TestContentPointReadAdmissionWireShapesAreSealed`) — exactly
  3 methods, no ledger/summary/snapshot surface.
- `content_read_admission_routes_test.go` — shared-double handler
  tests for all three routes through the real mux (full-row byte
  comparison, out-of-grant file 404, empty-grant entity 404, and the
  fixed-message fail-closed suite over all three routes), plus the
  route/spec-fragment parity test (every pinned path in the OpenAPI
  content fragment and served by `Mount`).
- Compile-time mirror assertion in the pin file:
  `var _ ContentPointReadAdmission = (*ContentReader)(nil)`.

## Satellite consumption

Build the satisfier with
`query.NewContentPointReadAdmissionWithReadStore(reader)` where
`reader` is the request-scoped `db.ReadStore`.

## Explicitly OUT (not widened by this pin)

- Grant checks, selector resolution, fixed failure messages, and
  QueryProfile (handler behavior, not port shape).
- Search legs (`SearchFileContent`, `SearchEntityContent`, twins).
- List legs (`ListRepoFiles`, `ListRepoEntities`, twins).
- Scoped-entity leg (`GetEntityContentInRepositories`).
- Ledger / stored-summary / snapshot surface (none on this family;
  sealed out regardless).
