# #6693 checklist step 3: `decisions/` leaf

Baseline: `origin/main`. Change: move
`go/internal/storage/postgres/decisions.go` and its test to the new
non-root package `go/internal/storage/postgres/decisions` (package clause
`decisionsstore`, no collision found via
`rg -n '^package decisionsstore$' go/ --glob '*.go'`). No SQL text, DDL,
predicate, or identifier changed; `decisionSchemaSQL`, the upsert/list
statement text, and the `DecisionStore`/`DecisionFilter`/`DecisionSchemaSQL`
exported surface are byte-identical, only re-homed.

Caller repointed: `go/internal/query/admin/store/postgres.go` imported the
root `postgres` package aliased `pgstatus` for `SQLDB` and now also imports
`github.com/eshu-hq/eshu/go/internal/storage/postgres/decisions` (identifier
`decisionsstore`, unaliased, matching `storage/postgres/semantic` and
`storage/postgres/scope`) for `NewDecisionStore`, `*DecisionStore`, and
`DecisionFilter`. `rg -n 'DecisionStore|DecisionFilter|DecisionSchemaSQL'`
across all of `go/` found no other caller of this family (the unrelated
`AdmissionDecisionStore` family stays in root).

The test file's use of root's private `proofResult{}` (a `sql.Result` stub)
was swapped for the exported equivalent `fake.Result{}` from
`internal/storage/postgres/fake`, per that package's intended use for tests
moving out of the root; `LastInsertId`/`RowsAffected` behavior is identical
(0, 1), so this is not a behavior change.

Root `README.md`'s two mermaid diagrams named `postgres.DecisionStore`
literally; both are updated to `decisionsstore.DecisionStore`. No other root
doc (`doc.go`, `AGENTS.md`) named this file or symbol by path.

No-Regression Evidence: `go test ./internal/storage/postgres/... -race
-count=1` and `go test ./internal/storage/postgres/decisions/... -race
-count=1` pass; `go test ./internal/query/... -count=1` passes (repointed
caller); `go build ./...` and `go vet ./...` pass; `go test -list '.*'
./internal/storage/postgres/decisions/` shows all six moved
`TestDecisionStore*` tests are discovered. The SQL text is unchanged so no
plan, index, or lifecycle proof is re-owed.

No-Observability-Change: this move relocates only the decision store, its
SQL text, and its DDL; no metric, span, log key, or status field is added,
removed, or renamed, and the admin store constructs the same store through
the same constructor signature.
