# AGENTS.md — internal/storage/postgres/admission

Scope: this directory only. Read `../AGENTS.md` and the repo root `AGENTS.md`
first for the shared #6693 domain-move rules.

- Package name is `admissionstore`. This package must not import the parent
  `postgres` package (root) — that would recreate the import cycle #6693
  splits away from.
- Own admission decision persistence only (`admission_decisions`,
  `admission_decision_evidence`). Do not add correlation-domain decision
  logic here; that belongs to `go/internal/reducer/admissiondecision` and its
  callers.
- `ListDecisions` requires domain, scope id, and generation id; do not relax
  that to allow an unbounded scan.
- Keep `EnsureSchema`'s DDL text in `schema.go` byte-identical to the
  migration it mirrors unless a real schema change is intended and reviewed
  as such — this is a move PR concern, not a general rule for later work here.
- `decisions_test_helpers_test.go` holds a package-local fake `ExecQueryer`
  double (`admissionDecisionTestDB`) that predates
  `internal/storage/postgres/fake`; it only borrows `fake.Result` for its
  `sql.Result` return. Do not duplicate `fake`'s `Route`/`Rows` machinery here
  if this file is ever rewritten — migrate it onto `fake.ExecQueryer` instead.
- Every exported symbol needs a real Go doc comment, not a restated name.
- Keep each file under 500 lines.
