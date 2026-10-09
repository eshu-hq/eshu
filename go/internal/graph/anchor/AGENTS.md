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
  you claim they agree. The live-backend CI job runs it from
  `specs/live-tests.v1.yaml` (Neo4j only; it self-skips elsewhere).
- **Fail closed for the tested shapes, and say so.** `CheckWriters` reports an
  unlabeled variable, a variable rebound to another label, an unprovable dynamic
  map or dynamic key, a parameter property map, a SET target that is not a plain
  variable, a label expression it cannot reduce, and an `id` key left in a
  MERGE or CREATE clause after the recognized patterns are masked. Do not turn
  any of them into a skip. It is not a full Cypher parser: do not write "fail
  closed" about a shape without a test row for it, and list a new blind spot in
  the README. A new false red is fixed by teaching `parse.go` the shape, with a
  test row first, not by an exception list.
- **A dynamic-label writer carries its own marker.** Put
  `// anchor-census: dynamic-label writer; label set bounded by <TestName>` within
  10 lines above the template, and make `<TestName>` a real test that proves the
  labels are anchor labels. The sweep fails on a template with no marker, on a
  marker naming no test, and on a marker with no template under it. Do not add a
  list of exceptions anywhere else.
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
