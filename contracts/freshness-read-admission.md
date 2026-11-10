# FreshnessReadAdmission v0 (core pin record)

Refs eshu-hq/eshu#9669. D13 Freshness-first Wave-1 core pin, per the
arbiter Wave-1 ruling (APPROVED-WITH-CONDITIONS). Minted core-side;
the satellite records this pin in a follow-up. A satellite PR
consuming an unminted pin is invalid. Arbiter review of this pin
precedes satellite termination of the freshness legs.

## Shape

Version: `FreshnessReadAdmissionContractVersion = "v0"`
(go/internal/query/freshness/read_admission.go).

Exactly 3 re-declared (never embedded) methods, sealed — no
`LedgerReadEnabled`, no `summaryReader`, no snapshot paths:

| # | Method | Mirrors (StatusStore) | Serves |
|---|--------|-----------------------|--------|
| 1 | `ComputeChangedSinceDelta(context.Context, changedsince.Filter) (changedsince.Summary, error)` | go/internal/storage/postgres/changed_since.go | `GET /api/v0/freshness/changed-since` |
| 2 | `ComputeServiceChangedSinceDelta(context.Context, changedsince.ServiceFilter) (changedsince.ServiceSummary, error)` | go/internal/storage/postgres/service_changed_since.go | `GET /api/v0/freshness/services/changed-since` |
| 3 | `ListGenerationLifecycle(context.Context, generation.LifecycleFilter) (generation.LifecyclePage, error)` | go/internal/storage/postgres/generation_lifecycle.go | `GET /api/v0/freshness/generations` |

Enforcement (all in go/internal/query/freshness):

- `read_admission_contract_test.go` — frozen method-set test plus
  version pin (`TestFreshnessReadAdmissionContractIsVersioned`).
- `read_admission_wire_shapes_test.go` — wire-shapes proof:
  `postgres.StatusStore` satisfies the pin, the
  `NewReadAdmissionWithReadStore(db.ReadStore)` constructor serves
  all three reads over nothing wider than the guarded `ReadStore`
  (fail-closed on unavailable reader), and the seal holds at
  exactly 3 methods with no ledger/summary/snapshot surface.
- Compile-time mirror assertion in the pin file:
  `var _ FreshnessReadAdmission = postgres.StatusStore{}`.

## Satellite consumption

Build the satisfier with
`freshness.NewReadAdmissionWithReadStore(reader)` where `reader`
is the request-scoped `db.ReadStore`. The store is ledger-off
(`LedgerReadEnabled` defaults false) with no stored-summary
reader, so reads serve the legacy two-generation diff exactly as
before and issue no ledger SQL; opting into the ledger stays a
core-side wiring decision with no wire change.

## Explicitly OUT (not widened by this pin)

- Changed-since link-ledger read path (`LedgerReadEnabled`, #7127 PR-3c).
- Stored-summary reader (`WithSummaryReader` / `StatusSummaryReader`, #7009).
- Snapshot legs (`ReadStatusSnapshot`, `ReadStatusSnapshotFiltered`,
  `ReadRawSnapshot`) — covered by `status.StatusReadAdmission` v0.
- Grant checks, failure messages, `QueryProfile` — handler behavior,
  no method set to freeze.

## Invariants held by this pin (arbiter conditions)

- Zero edits to `querycontract/ports.go`.
- Zero drift in the frozen sets (`api_boundary`,
  `read_store_contract`); no `CoreReadContractsVersion` bump.
- Unwired seam: no handler, route, or caller depends on the pin yet.
  Rollback = revert this pin; rollback moves no traffic and changes
  no data.
