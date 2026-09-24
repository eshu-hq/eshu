# internal/storage/postgres/admission

`admissionstore.AdmissionDecisionStore` persists shared reducer admission
decisions against the `admission_decisions` and `admission_decision_evidence`
tables. It is one #6693 leaf split out of `go/internal/storage/postgres`
(the "root" package).

## Purpose

Correlation reducer domains (deployable-unit, package-supply-chain, cloud
inventory, and others) each decide whether a candidate anchor/target pair is
eligible for a canonical graph or content write. That decision — admitted,
rejected, ambiguous, stale, missing evidence, permission hidden, unsupported,
or unsafe — plus the evidence behind it, is shared, cross-domain state: the
API and MCP admission-decision read surfaces answer from it regardless of
which reducer domain produced the row. This package is the single write and
read path for that shared table pair so no domain package needs its own copy
of the schema or SQL.

## Ownership boundary

This package owns admission decision persistence only. It does not decide
admission states — `go/internal/reducer/admissiondecision` and its callers
(`go/cmd/reducer/admission_decision_wiring.go`) do that and call
`UpsertDecision`/`InsertEvidence` with an already-decided
`AdmissionDecision`. `go/internal/query/admission_decision_store.go` is the
read caller behind the API/MCP admission-decision routes. This package must
not import the parent `postgres` package.

## Exported surface

- `AdmissionDecisionStore` / `NewAdmissionDecisionStore` -- the store type and
  constructor, backed by `db.ExecQueryer`.
- `EnsureSchema` -- applies `AdmissionDecisionSchemaSQL()` (the
  `admission_decisions` and `admission_decision_evidence` DDL).
- `UpsertDecision` / `InsertEvidence` -- write one decision (validated against
  the closed `AdmissionDecisionState` vocabulary) or a batch of evidence rows.
- `ListDecisions` / `ListEvidence` -- bounded reads; `ListDecisions` requires a
  non-empty domain, scope id, and generation id, and both clamp their `Limit`
  between 1 and 500.
- `AdmissionDecision`, `AdmissionDecisionEvidence`, `AdmissionDecisionFilter`,
  `AdmissionDecisionState` (+ `AdmissionDecisionStateValues`),
  `AdmissionDecisionSourceHandle`, `AdmissionDecisionCanonicalWrite`,
  `AdmissionDecisionNextAction` -- the persisted shapes.

## Dependencies

`database/sql`, `context`, `encoding/json`, `fmt`, `strings`, `time` (standard
library), plus this repo's `internal/storage/postgres/db` (the `ExecQueryer`
contract) and, in tests only, `internal/storage/postgres/fake`
(`fake.Result`).

## Telemetry

None in this package: it is a persistence layer with no metric, span, or log
of its own. Callers that decide admission states and callers that serve API
requests own the operator-facing signal for those actions.

No-Observability-Change: this move relocates existing store code and its
tests with no behavior change; no metric, span, log field, or telemetry
attribute changed.

## Gotchas / invariants

- `ListDecisions` and `ListEvidence` fail closed on an unbounded filter
  rather than falling back to a full-table scan.
- `UpsertDecision` validates `AdmissionDecisionState` before any write; an
  unknown state writes nothing.
- `admissionDecisionTestDB` in `decisions_test_helpers_test.go` is a
  package-local fake predating the shared `fake` package; only its `sql.Result`
  return uses `fake.Result` so the type is visible outside `postgres` root's
  own test files.
