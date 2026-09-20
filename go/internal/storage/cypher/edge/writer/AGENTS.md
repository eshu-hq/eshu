# AGENTS.md — Cypher edge-writer leaf guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../../AGENTS.md` for Cypher storage conventions.
3. `writer.go`, `retract.go`, and
   `retract_scope.go` for the write/retract/narrowing core.
4. `retract_narrowing_test.go` for the sanctioned-call-site
   guard before touching any whole-scope path.

## Invariants

- Keep every Cypher template byte-identical when moving code; statement
  or predicate changes need their own issue with `EXPLAIN` and contention
  proof per `cypher-query-rigor`.
- Preserve the single sanctioned whole-scope narrowing call site; the
  narrowing guard fails the package if a second caller appears.
- Keep the shared statement constants exported until the canonical leaf
  moves; the canonical writers and the `edge/materialized` sibling read
  through them.
- Keep the package clause as `package writer`; external callers use the
  `edgewriter` alias, leaf tests stay in-package.
- Never import the `edge/materialized` sibling or the parent-test fakes
  from here; the duplicated test harness files name their root originals.
