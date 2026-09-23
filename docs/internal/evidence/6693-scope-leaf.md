# #6693 scope/ leaf

Change: checklist step 1 of `docs/internal/design/6693-postgres-target-tree.md`.
It adds the `go/internal/storage/postgres/scope` leaf (`package scopestore`)
and hoists the two `scope/` rows of the "Prerequisite hoists" table into it:

- `scopeSourceKey` (from `ingestion_queries.go`) becomes `scopestore.SourceKey`.
- `deferredScopedFactOwnRepoIDFromScope` and its private
  `gitRepositoryScopePrefix` constant (from `ingestion_backfill_deferred_regex.go`)
  become `scopestore.RepoIDFromScopeID`.

Callers repointed: `ingestion_queries.go`, `deferred_maintenance_lock.go`,
`ingestion_backfill_deferred_facts.go` and `relationship_reference_keys.go`, plus
three root tests. No caller outside `internal/storage/postgres` referenced
either helper; `rg` across the repo finds no remaining old name outside the
design doc's mapping table. The unit tests moved with the code:
`ingestion_scope_source_key_test.go` became `scope/source_key_test.go`, and
`TestDeferredScopedFactOwnRepoIDFromScope` became `TestRepoIDFromScopeID` in
`scope/repo_id_test.go`.

The old root doc comment for the repo-ID helper sat on the constant, not the
function, so `go doc` showed nothing for the function. The moved function
now carries it.

Adding the `scope` directory makes dirgate's sibling-name rule flag root's
`scope_quiescence.go`, whose mapped destination is
`scope/completion/quiescence.go` (checklist step 22). That file carries a
`//nolint:dirgate` marker naming step 22 until the move lands; the gate
failed without the marker and passes with it.

No-Regression Evidence: both function bodies are unchanged apart from the
exported names, so no SQL text, query, lock, lease, batch size, worker count or
graph write changes, and the persisted `ingestion_scopes.source_key` value and
the `$6` performance hint are the same strings as before. Root's non-test file
count is unchanged (362); the new files are under `scope/`. After the final
edit, from `go/`: `go build ./...` and `go vet ./...` exit 0; `go vet -tags
"integration perf5854_ack perf5740_completion perf6785_wait"
./internal/storage/postgres/...` exits 0; `go test
./internal/storage/postgres/... ./internal/collector/repo/git/... -race
-count=1` passes, including the three moved tests in `scope/`. `go test ./internal/storage/postgres/scope/... -list '^(TestRepoIDFromScopeID|TestSourceKeyUsesMetadataSourceKeyForRepositoryScope|TestSourceKeyFallsBackToScopeIDWhenMetadataSourceKeyMissing)$' -count=1` prints all three moved names, and `go test ./internal/storage/postgres -list '^(TestDeferredScopedFactOwnRepoIDFromScope|TestScopeSourceKeyUsesMetadataSourceKeyForRepositoryScope|TestScopeSourceKeyFallsBackToScopeIDWhenMetadataSourceKeyMissing)$' -count=1` prints none of the three old root names, so the tests moved rather than being duplicated or dropped.

No-Observability-Change: both helpers are pure string derivations with no
metric, span or log of their own; no metric, span, log key or status field is
added, removed or renamed.
