# Neo4j full-text indexes use the modern CREATE form (#7675)

Every full-text index shipped two statements: the `primary`
(`CALL db.index.fulltext.createNodeIndex(...)`) and the `fallback`
(`CREATE FULLTEXT INDEX ... IF NOT EXISTS`). The executor ran the
primary on every backend and the fallback on any failure, and the
statement list carried both. Neo4j removed the procedure API in 5.0,
so on the pinned backend each fresh apply wasted two round trips and
logged two `graph schema statement failed` warnings before the
fallbacks created the indexes.

The dialect now names the forms each backend runs
(`schemaDialect.fulltextForms`): Neo4j runs only the modern `CREATE
FULLTEXT INDEX` form, NornicDB only the procedure form. The executor
loops the forms (first success wins; context failures still fail
fast), and the statement list, the statement total, and the adoption
parser all use the same per-backend forms. The `skipFulltextFallback`
flag is gone: there is no fallback to skip. NornicDB output is
byte-identical: its statement list is unchanged and its fingerprint
stays `8dfe89b17682…03944`.
The Neo4j fingerprint moves from `675dafc901ff…8d54` to
`ac31ec4947f1…9c3e`, and the predecessor stays compatible: the two
indexes keep their names (`code_search_index`, `infra_search_index`),
labels, and properties, so writers and readers see the same graph.

## Proof

- `TestSchemaStatementsForBackendUsesModernFulltextFormOnNeo4j`,
  `TestSchemaStatementsForBackendKeepsProcedureFulltextFormOnNornicDB`,
  `TestEnsureSchemaNeverAttemptsTheRemovedProcedureOnNeo4j`, and
  `TestEnsureSchemaKeepsProcedureOnlyFulltextOnNornicDB`
  (`go/internal/graph/schema_fulltext_forms_test.go`, new). RED on the
  base (Neo4j list carries the procedure form and the executor attempts
  it); GREEN on this change: the Neo4j statement list holds exactly the
  two `CREATE FULLTEXT INDEX` statements, the NornicDB list exactly the
  two procedure calls, and a recording executor sees no
  `db.index.fulltext.createNodeIndex` on Neo4j.
- `TestSchemaApplicationsDeclareCompatibilityDecision/neo4j`: bumped to
  the new digest with `graphSchemaNeo4jPreFulltextModernFormFingerprint`
  admitted; the NornicDB legs are untouched.
- Adoption: `TestRunDoesNotAdoptNeo4jSchemaThatStillHasRetiredConstraints`
  was RED (`cannot adopt unsupported graph schema statement "CREATE
  FULLTEXT INDEX ..."`); the adoption parser now resolves the modern
  form (quoting-normalized) and the suite is GREEN.
- Live (scratch test, not committed; local disposable
  `neo4j:2026-community@sha256:eabfbb04...` container, fresh database,
  `EnsureSchemaWithBackendStrict` + `openNeo4j` + real inspector):
  fresh apply executed 259 statements with zero
  `db.index.fulltext.createNodeIndex` attempts, exactly two `CREATE
  FULLTEXT INDEX`, no WARN in the captured debug log, and both indexes
  present under inspection. Re-apply through the adoption skip path
  executed only the 5 idempotent `DROP CONSTRAINT ... IF EXISTS`
  retirement statements; every CREATE was skipped by name.

No-Regression Evidence: per fresh Neo4j apply, full-text DDL goes from
4 executions (2 rejected procedures + 2 creates) to 2 executions (2
creates), measured on the live container above (259 statements total,
backend `neo4j:2026-community`, debug log captured, both indexes
inspected present). Re-apply behavior is unchanged apart from the
parser now resolving the two full-text names (5 idempotent drops, 0
CREATEs). NornicDB executes the same 2 procedure calls as before; its
fingerprint is unmoved and the full `go/internal/graph` suite is
GREEN. No MERGE/MATCH identity, constraint, or read-path change, so no
query-plan or corpus-shape delta is possible.

Observability Evidence: a fresh Neo4j bootstrap no longer logs the two
spurious `graph schema statement failed` warnings (phase
`fulltext_primary`) that the rejected procedure attempts produced. The per-statement
`graph_backend` / `schema_phase` / `statement_index` /
`statement_total` shape is unchanged; `statement_total` now counts only
the forms the backend runs. An operator grepping a bootstrap log for
`WARN` sees none on a healthy apply; any remaining warning is a real
failure.
