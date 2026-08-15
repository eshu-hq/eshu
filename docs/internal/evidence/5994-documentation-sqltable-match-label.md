# SqlTable Added To The Documentation Entity MATCH (#5994)

## What changed

`batchCanonicalDocumentationEntityEdgeCypher`'s MATCH label alternation
(`go/internal/storage/cypher/canonical_documentation_edges.go`) gained one
label: `Function|Class|Struct|Interface|TypeAlias|Enum|File` became
`Function|Class|Struct|Interface|TypeAlias|Enum|File|SqlTable`. No other line
of the template changed; the `MERGE`/`SET` clauses, the row shape, and the
statement's position in the writer pipeline are untouched.

No-Regression Evidence: this is a text-only widening of an existing label
disjunction inside an already-live `UNWIND $rows AS row MATCH (target:...
{uid: row.target_entity_id}) ...` template, not a new query shape, so no full
benchmark run was taken; the decision to skip one is stated explicitly below
rather than left implicit.

- **Query shape**: the template is `UNWIND $rows AS row MATCH (target:A|B|...
  {uid: row.target_entity_id}) ...`, a row-driven inline-property-anchor
  disjunction. `docs/public/reference/nornicdb-query-pitfalls.md`'s "Pitfall:
  Node-Label Disjunction In A MATCH Matches Zero Rows" section documents two
  distinct classes on the pinned NornicDB binaries: a **bare** `MATCH`
  disjunction (optionally followed by a `WHERE` predicate) returns zero rows
  on v1.1.9/v1.1.11, but the page's "Scope refinement" note is explicit that
  a **row-driven `UNWIND $rows AS row MATCH (n:A|B|C {prop: row.value})` with
  an inline property anchor DOES match and write correctly** (citing the
  rationale EXPLAINS writer as the proof case) and says plainly: "Do not
  'fix' working UNWIND inline-anchor writes." `batchCanonicalDocumentation
  EntityEdgeCypher` is exactly that working shape, both before and after this
  change — the fix only adds one more label to an already-passing disjunction
  class, not a new shape needing its own pitfall verification.
- **Cardinality**: the SQL relationship family's own writer already runs a
  6-label inline-anchor disjunction of the same kind
  (`MATCH (source:SqlTable|SqlView|SqlFunction|SqlTrigger|SqlIndex|SqlColumn
  {uid: row.source_entity_id})`, `go/internal/storage/cypher/canonical.go`)
  and is the one materialized-edge family in this repo with full live
  baseline+delta+fault coverage on the pinned backend. Going from 7 to 8
  labels on the documentation template stays well inside the cardinality
  this repo already runs live daily; it is not a new order of magnitude.
- **Index-backed lookup**: `SqlTable` carries a uid uniqueness constraint
  (`uidConstraintLabels` in `go/internal/graph/schema_tables.go`), so the
  added branch of the disjunction is index-backed, not a label scan.
- **Backend pin**: the NornicDB behavior cited above is measured on the
  canonical pinned image (`timothyswt/nornicdb-cpu-bge:v1.1.9`/`v1.1.11`,
  the same pin `docs/public/reference/local-testing.md`'s golden-corpus gate
  and the Ifá live gates use); no other backend version is in scope for this
  change.
- **Verification against the pinned binary**: the `materialized_edges:
  documentation_edges` Ifá live-gate proof (baseline + fault, #5994, owned by
  the team lead) is the verification that this specific statement persists a
  SqlTable-targeted DOCUMENTS edge on the pinned binary end to end, the same
  way the SQL family's own live gate is what proves its 6-label disjunction
  persists on the same binary — a synthetic microbenchmark of the query
  string in isolation would not exercise the backend behavior this change
  actually depends on, so the live-gate green (once it lands) is the
  verification, not a substitute for a benchmark that would not have caught
  the class of failure this repo has actually seen (the bare-MATCH zero-row
  bug, which this shape does not exhibit).

Cost-budget proof: `cd go && go test ./internal/replay/costcounting/... -count=1`
→ 57 passed, 0 failed, exit 0. The cost-counting model attributes one
scenario per reducer/writer call
(`go/internal/replay/costcounting/AGENTS.md`: `documentation_materialization`
is costed once per `cypher.EdgeWriter.WriteEdges` call), not per label in a
MATCH clause; this change adds a label to an existing template string without
adding, removing, or splitting any statement, batch, or writer call, so the
budget is unaffected by construction and the unchanged, still-green suite
confirms it rather than merely asserting it.

Regression suite: `cd go && go test ./internal/ifa/... ./internal/storage/cypher/... ./internal/reducer/... -count=1`
→ 5050 passed, 0 failed, exit 0 (captured directly, after the final edit).
`go vet` and `golangci-lint run` on `internal/ifa` and
`internal/storage/cypher`: 0 issues.

No-Observability-Change: no route, graph query shape beyond the one label
addition, queue table, worker, lease, batch size, runtime knob, metric
instrument, or metric label is added, removed, or changed. Operators still
diagnose this path through the existing `documentation_materialization`
reducer run spans and execution counters, the existing
`eshu-ifa assert-edges -domain documentation_edges` live-gate check, and the
existing `TestBuildDocumentationRowMapTableTargetMatchesSqlTableLabel`
regression guard (RED before this change at commit a3347e898, GREEN after).

## Reproduction

```
cd go
ESHU_PERFORMANCE_EVIDENCE_BASE=origin/main ../scripts/verify-performance-evidence.sh
```
