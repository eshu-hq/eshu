# AGENTS.md — Postgres code-taint projected ledgers guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../../AGENTS.md` for Postgres storage conventions.
3. `interprocedural_edge.go` and `projected_node.go` for the two ledger
   stores.
4. `interprocedural_edge_test.go` and `projected_node_test.go` for the store
   contract, including the migration-SQL parity tests.
5. `internal/reducer/AGENTS.md` (#4893) for how the reducer drives these
   ledgers end to end.

## Invariants

- Keep the DDL, upsert batching, in-batch dedupe, and enumeration/prune
  predicates byte-identical when moving or refactoring code; predicate or
  lifecycle changes need their own issue with EXPLAIN ANALYZE and contention
  proof per `eshu-postgres-rigor`.
- Never edit the migration files
  (`../../migrations/044_code_interproc_projected_edge.sql`,
  `../../migrations/045_code_taint_evidence_projected_node.sql`); their
  checksums are load-bearing. Change `CodeInterprocProjectedEdgeSchemaSQL()`
  / `CodeTaintEvidenceProjectedNodeSchemaSQL()` only alongside a new
  migration, and keep both migration-parity tests green.
- Keep the package clause as `package taintstore`; callers import the
  `storage/postgres/code/taint` path without an alias (its declared name
  already differs from `internal/reducer/code/taint`'s `taint`, so both can
  be imported in the same file without a collision).
- Never import the parent `postgres` package from here.

## Common changes

- Change a ledger's DDL only with its `SchemaSQL`-parity test and migration
  file updated together, keeping the Go constant and the migration SQL
  byte-identical.
- Change enumeration or prune predicates only with the corresponding query
  shape test updated and an idempotency proof for the batched `ON CONFLICT`
  upsert.

## Failure modes

- Importing the parent `postgres` package creates an import cycle.
- Writing the graph before the ledger record completes breaks the superset
  invariant retraction depends on, risking an orphaned graph edge or node.
- Pruning stale rows for uids that were not actually retracted from the
  graph in the same batch would let ledger rows and graph state diverge.

## Verification

From `go/`, run:

```bash
go test ./internal/storage/postgres/code/taint/... -count=1
go vet ./internal/storage/postgres/code/taint/...
```

Run `scripts/verify-package-docs.sh` from the repository root.
