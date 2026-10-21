# Kubernetes leaf guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../../AGENTS.md` for Cypher storage conventions.
3. `namespace.go` for the namespace writer (upsert variants, stale-edge
   retract, absent-node sweep) and `workload.go` for the workload writer.

## Ownership

- This package owns the Kubernetes node writer types, their Cypher
  templates, row shaping, batching, and dispatch order.
- The parent `cypher` package owns `Statement`, `Executor`, operation and
  metadata keys, batching helpers, and retry wrapping.

## Invariants

- Keep every Cypher template byte-identical when moving code; statement
  or predicate changes need their own issue with `EXPLAIN` and contention
  proof per `cypher-query-rigor`.
- Keep the stale `TARGETS_ENVIRONMENT` retract ahead of the upserts and on
  sequential `Execute`, never `ExecuteGroup` (NornicDB DELETE pitfall).
- Never route an unbound namespace row (empty `environment`) through the
  with-environment template (#5434).
- Keep the package clause as `package kubernetes`; external callers use the
  `k8snodewriter` alias, leaf tests stay in-package.
- Never import a sibling leaf from here; this leaf imports only the parent
  cypher package, and the parent never imports this leaf.
- Keep the template constants exported: the `edge/materialized` sibling
  reads through them.
