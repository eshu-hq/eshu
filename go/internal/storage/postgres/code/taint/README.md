# Postgres code-taint projected ledgers

## Purpose

This package owns the two durable ledgers the code-taint / interprocedural
value-flow retraction path (#4893) reads and writes: the source Function uid
of every projected `TAINT_FLOWS_TO` edge, and the node uid of every projected
`CodeTaintEvidence` node. Each ledger lets the reducer retract stale graph
state by an indexed uid lookup instead of scanning the whole graph on
NornicDB.

## Ownership boundary

This package owns the `code_interproc_projected_edge` and
`code_taint_evidence_projected_node` tables, their DDL, batched upsert, and
enumeration/prune reads. It is distinct from
`internal/reducer/code/taint`, which owns the value-flow evidence
materialization, backfill, and stale-cleanup domain logic that calls through
the `InterprocProjectedEdgeLedger` / `ProjectedNodeLedger` interfaces these
stores satisfy. The parent `postgres` package keeps the migration files
(`044_code_interproc_projected_edge.sql`,
`045_code_taint_evidence_projected_node.sql`) and constructs these stores for
`cmd/reducer` wiring; this package must not import it back.

## Exported surface

- `CodeInterprocProjectedEdgeStore` with
  `NewCodeInterprocProjectedEdgeStore(database db.ExecQueryer)`:
  `EnsureSchema`, `RecordProjectedEdges`, `ListSourceUIDsForScopes`,
  `ListSourceUIDsForSource`, `ListStaleSourceUIDs`, `PruneForScopes`,
  `PruneForSource`, `PruneStaleForUIDs`, `LedgerHasRowsForSource`, and
  `CodeInterprocProjectedEdgeSchemaSQL()` for the DDL.
- `CodeTaintEvidenceProjectedNodeStore` with
  `NewCodeTaintEvidenceProjectedNodeStore(database db.ExecQueryer)`:
  `EnsureSchema`, `RecordProjectedNodes`, `ListNodeUIDsForScopes`,
  `ListStaleNodeUIDs`, `PruneForScopes`, `PruneStaleForUIDs`,
  `LedgerHasRowsForSource`, and
  `CodeTaintEvidenceProjectedNodeSchemaSQL()` for the DDL.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/storage/postgres/db` for the shared `ExecQueryer` contract.
- Its own tests additionally import the parent `internal/storage/postgres`
  package (`postgres.MigrationSQL`) to prove each `SchemaSQL()` constant is
  byte-identical to its migration file; that is a test-only, one-directional
  dependency and does not create an import cycle in production code.

## Telemetry

None. Both stores execute bounded, parameterized SQL through the injected
database handle; the reducer's value-flow fixpoint projector and stale
cleanup runner that call them own observability for the domain (see
`internal/reducer/AGENTS.md`, #4893).

No-Observability-Change: this extraction moves only the two durable ledgers;
no metric, span, log field, worker, queue, lease, or runtime knob changed.

## Gotchas / invariants

- `RecordProjectedEdges`/`RecordProjectedNodes` must run, and complete,
  before the corresponding graph write: the ledger is a superset invariant,
  and under-inclusion orphans a graph row retraction can no longer find.
- Both upserts batch at 500 rows and de-duplicate uids within a batch before
  insert to avoid Postgres `SQLSTATE 21000` ("more than one row returned by
  a subquery used as an expression" from `ON CONFLICT` matching a duplicate
  key twice in the same statement).
- `ListStaleSourceUIDs`/`ListStaleNodeUIDs` and `PruneStaleForUIDs` are
  generation-scoped (`generation_id <> current`): a stale-cleanup pass only
  ever prunes ledger rows for uids it actually retracted from the graph in
  the same bounded batch, never the whole stale set at once.
- Do not import the parent `postgres` package: that is an import cycle.
  `cmd/reducer` constructs both stores and wires them into
  `internal/reducer/code/taint`'s backfillers, evidence materializers, and
  `internal/reducer.CodeValueFlowStaleCleanupRunner`.

No-Regression Evidence: `go test ./internal/storage/postgres/code/taint/...
-count=1` covers schema-SQL parity, in-batch dedupe/blank skipping,
enumeration query shapes, and prune shapes for both ledgers. Full runtime
evidence lives in `go/internal/reducer/AGENTS.md` (#4893).

## Related docs

- [Postgres storage](../../README.md)
- [Shared database contracts](../../db/README.md)

## Verification

From `go/`, run:

```bash
go test ./internal/storage/postgres/code/taint/... -count=1
go vet ./internal/storage/postgres/code/taint/...
```

Run `scripts/verify-package-docs.sh` from the repository root.
