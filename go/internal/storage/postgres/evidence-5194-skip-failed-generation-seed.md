# #5194 skip-failed-generation seed evidence

Performance Evidence: this is a correctness fix, not a performance-motivated
rewrite — `SeedSearchVectorScopeState`'s startup `INSERT ... SELECT` had no
filter excluding repository scopes with a failed ingestion
(`active_generation_id IS NULL`), so the NOT NULL constraint on
`generation_id` aborted the whole batch on the first such scope. The fix adds
one `AND s.active_generation_id IS NOT NULL` predicate to the existing
`WHERE` clause and one new `SELECT count(*)` query
(`countFailedGenerationRepositoryScopesSQL`) for operator telemetry. Per the
Prove-The-Theory-First behavior-change rule this proves the intended delta
(seeder no longer aborts on a failed scope), not before/after identity with
the old crashing query.

No-Regression Evidence: measured against the real local stack's Postgres
(`eshu-postgres-1`, port 15439, real ~900-repo local corpus, Postgres in
docker-compose), `EXPLAIN ANALYZE` on 2026-07-13:

- `ingestion_scopes` repository-scope rows at proof time: `802` active/healthy,
  `123` failed (`no_gen=true`), `925` total — small, fully control-table
  sized, not a fact/content-scale table.
- New count query alone (`countFailedGenerationRepositoryScopesSQL`): `Seq Scan
  on ingestion_scopes`, `Execution Time: 0.269 ms`, `rows=124`, `Buffers:
  shared hit=50`.
- Filtered candidate-set alone (`active_generation_id IS NOT NULL` arm):
  `Execution Time: 0.211 ms`, `rows=801`, `Buffers: shared hit=50`.
- Full production `seedProjectionStateSQL` statement (the actual `INSERT ...
  SELECT` with the new filter, including the `NOT EXISTS` anti-join and the
  per-scope document-count subplan), run inside `BEGIN; ...; ROLLBACK;` so the
  real DB was not mutated by this proof: `Insert on
  eshu_search_document_projection_state`, `Hash Anti Join` against `823`
  already-seeded rows, `Execution Time: 0.579 ms`, `Planning Time: 2.657 ms`,
  `Buffers: shared hit=82`. `rows=0` because the corpus was already fully
  seeded from prior runs (idempotent `NOT EXISTS` guard) — this is the correct
  steady-state shape the reducer sees on every restart.
- Both the count query and the seed query are `Seq Scan`s over the full
  `ingestion_scopes` repository-scope set (hundreds of rows, not fact-table
  scale), so cost is bounded by control-table size and runs in sub-millisecond
  time regardless of the added predicate. No index was added or needed.

Observability Evidence: `SeedSearchVectorScopeState` now returns
`SeedSearchVectorScopeStateResult{ProjectionRowsSeeded, FailedScopesSkipped}`
instead of discarding both `ExecContext` results, and
`go/cmd/reducer/main_helpers.go`'s `seedSearchVectorScopeState` logs both
`projection_rows_seeded` and `failed_generation_scopes_skipped` on the
existing "search vector scope state seeded" startup log line, so an operator
can distinguish "fully seeded" from "N scopes excluded pending re-ingestion"
instead of one opaque success message. No new metric, span, route, worker,
lease, batch size, or queue-state change — this is a log-field addition to an
existing startup log line only.

Local proof (see PR description for full list):
`ESHU_SEARCH_VECTOR_SCOPE_STATE_LIVE=1 ESHU_POSTGRES_DSN=<real dsn> go test
./internal/storage/postgres -run
TestSeedSearchVectorScopeStateSkipsFailedGenerationScopeLive -count=1 -v`
against the same real corpus: PASS.
