# Agent instructions: audit

Read `doc.go` and `README.md` first.

## Invariants

- This package imports no handler or store package outside tests. The tests
  import the supply-chain leaf packages and `capabilitycatalog`; the
  non-test code imports only `facts`, `storage/postgres/array`, and
  `storage/postgres/db`.
- A new classifier rule needs a seeded RED/GREEN pair in `classify_test.go`:
  a statement that fails under a declaration that omits the store, and passes
  under one that names it.
- A waiver list may only shrink. Adding a probe or an owner and deleting the
  waiver line is the normal change; raising a ceiling constant needs a stated
  reason in the PR.
- The structural-table allow-list in `classify.go` names tables that scope or
  authorize a read but never answer one. Each entry states why. Do not add a
  table to hide a read that should be declared.
- A fact_records read the classifier cannot attribute to a kind is a failure,
  not a skip.
- A seeded RED/GREEN case lands with every classifier rule that can drop a read:
  dead-branch removal, system catalogs, the watermark alias, structural tables,
  and each parseable shape in `shapes_test.go`.
- A readiness-style read that reaches a response is declared, never listed as
  "supporting". The supporting list holds only tables no probe reaches live.
