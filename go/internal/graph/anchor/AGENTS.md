# AGENTS.md — internal/graph/anchor guidance for LLM assistants

## Read first

1. `go/internal/graph/anchor/README.md` — the reachability definition.
2. `docs/internal/evidence/7212-wide-entity-anchor.md` — why the set must stay
   closed and what the 2026-10-08 census measured.
3. `go/internal/graph/schema_tables.go` and `schema_id_constraint.go` — the
   uid and id constrained label sets this package unions.

## Invariants

- **One definition.** `Classify` and `CensusCypher` must agree. Change one and
  the other in the same commit, and run `census_live_test.go` on Neo4j before
  you claim they agree (CI's live-backend job runs it).
- **Fail closed.** `CheckWriters` reports an unlabeled variable and an
  unprovable dynamic map as findings. Do not turn either into a skip. A new
  false red is fixed by teaching `parse.go` the shape, with a test, not by an
  allowlist.
- **A gate that sees nothing must fail.** Zero statements, or zero id writes,
  is a failure in the gate phase. Keep that when you edit the phase.
- **No ids in output.** Failures print label sets and counts, never an entity
  id, a path, or a parameter value.
- **No telemetry here.** Emission belongs to the runner that calls the census.

## Common changes

- **A new constrained label** needs no edit: `Labels` derives from the schema.
- **A new Cypher shape the analyzer misreads** → add a row to `TestCheckWriters`
  first (RED), then extend `parse.go`.
- **A dynamic-label writer** → the static sweep cannot see it; make sure the
  corpus replay exercises it.
