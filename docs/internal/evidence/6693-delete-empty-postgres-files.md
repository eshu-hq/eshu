# #6693 D5: delete six empty storage/postgres files

Baseline: `origin/main` `2adeead47`. Change: delete six files in
`go/internal/storage/postgres` that each held only the license header and
`package postgres`: `admin_replay_request_schema.go`,
`collector_evidence_summary_schema.go`,
`collector_generation_dead_letter_schema.go`,
`graph_schema_applications.go`, `schema_fact_records_sbom.go` and
`schema_fact_records_service_catalog_indexes.go`. The DDL they once carried
already lives in `migrations/*.sql`, which this change does not touch.

No-Regression Evidence: the deleted files declared no identifier, so no
statement, query, lock, lease, batch size, worker count or graph write changes.
`git show 2adeead47:<path>` for each file shows four lines: two license
comment lines, a blank line, and the package clause. After the change,
`go build ./internal/storage/postgres/...` and
`go vet ./internal/storage/postgres/...` pass, as do the package's
schema and bootstrap tests
(`go test ./internal/storage/postgres -run 'Schema|Bootstrap' -count=1`).
The migration set, and so every checksum `BootstrapDefinitions` computes, is
byte-identical because no `.sql` file changed. The dirgate row for
`internal/storage/postgres` moves from 368 to 362 files, and
`scripts/verify-dirgate.sh --digest internal/storage/postgres` reports the
new count and digest.

No-Observability-Change: no runtime code path, metric, span, log key or status
field is added, removed or renamed; the deleted files contained no executable
code.
