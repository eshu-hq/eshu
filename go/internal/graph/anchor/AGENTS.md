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
- **The census is the authority; `CheckWriters` is a heuristic pre-filter.**
  The required `graph/anchor_census` check after the replay, and the reducer
  gauge, decide whether an unanchored id-bearing node exists. `CheckWriters`
  reports the shapes its test rows cover (the README names them). Do not turn a
  reported shape into a skip. Do not write a categorical claim about what the
  analyzer or the sweep catches: state a shape only with its test function
  beside it, and add a newly found miss to the README's known blind spots
  (not exhaustive) with a row in `TestCheckWritersKnownBlindSpots` or
  `TestSweepKnownBlindSpots`. A new false red on the production sweep is fixed by
  teaching `parse.go` the shape with a test row first, never by an exception
  list.
- **A dynamic-label writer carries its own marker.** Put
  `// anchor-census: dynamic-label writer; label set bounded by <TestName>` within
  10 lines above the template, and make `<TestName>` a real test, defined in the
  same directory as the marker, that proves the labels are anchor labels. A
  marker pairs 1:1 with the nearest template below it. The sweep reports a
  template with no marker of its own, a marker naming no test in its directory,
  and a marker with no template of its own under it. Do not add a list of
  exceptions anywhere else.
- **A gate that sees nothing must fail.** Zero statements, or zero id writes,
  is a failure in the gate phase. Keep that when you edit the phase.
- **No ids in output.** Failures print label sets and counts, never an entity
  id, a path, or a parameter value.
- **No telemetry here.** Emission belongs to the runner that calls the census.

## Common changes

- **A new constrained label** needs no edit: `Labels` derives from the schema.
- **A new Cypher shape the analyzer misreads** → add a row to `TestCheckWriters`
  first (RED), then extend `parse.go`.
- **A dynamic-label writer** (a template whose node label is a placeholder) →
  put `// anchor-census: dynamic-label writer; label set bounded by <TestName>`
  beside it, within 10 lines above, with `<TestName>` a real test in the same
  directory that proves each label the writer can use is an anchor label. The
  sweep fails without the marker, without the test, or when two writers share
  one marker. The corpus replay still has to exercise the writer.
