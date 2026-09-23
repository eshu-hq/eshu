# #6693 db/scalars prerequisite hoists

Baseline: `origin/main` `e05b67e99`. Change: the `db/` and `scalars/` rows
of the "Prerequisite hoists" table in
`docs/internal/design/6693-postgres-target-tree.md`. Seven private helpers
move byte-identically out of `go/internal/storage/postgres` (package
`postgres`) into the two existing leaf packages, exported so root and later
domain packages can call them:

- `go/internal/storage/postgres/db`: `SearchIndexTermCopyUnsupportedError`
  (from `adapters.go`, with its `Driver` field and methods) and
  `WithQuerySummary` / `QuerySummaryFromContext` (from
  `status_read_telemetry.go`, moved together with the context key that backs
  them).
- `go/internal/storage/postgres/scalars`: `DurationFromSeconds` (from
  `status.go`), `NullableTimeUTC` (from `status_aws_cloud.go`),
  `NullableTime` (from `workflow_control_helpers.go`), `StringMapToAny`
  (from `ingestion_queries.go` -- the design doc's table names `ingestion.go`,
  but the current source has this helper in `ingestion_queries.go`; `rg`
  confirmed there is no separate `stringMapToAny` in `ingestion.go`), and
  `CleanStringSet` (from `aws_cloud_runtime_drift_findings.go`, shared by the
  aws/multi/terraform drift-finding filters).

Every root call site was found with `rg` across all of `go/` (not just the
postgres tree) and repointed to the exported name; no caller outside
`internal/storage/postgres` referenced any of the seven helpers. The one root
type assertion / `errors.As` user of the copy-unsupported error
(`instrumented.go`; a second consumer, `isSearchIndexTermCopyUnsupported` in
`reducer/eshusearch/eshu_search_document_index_writer.go:226`, matches only the
`UnsupportedSearchIndexTermCopy() bool` method and never imports the type, so
renaming that method would break it) and the `TestSearchIndexTermCopyUnsupportedErrorIsTyped`
test both still match the moved type's `UnsupportedSearchIndexTermCopy() bool`
method. No collisions: neither leaf declared any of the seven exported names
before this change (checked by listing `^func |^type ` in both packages).

Unit tests for each hoisted helper moved with it into the leaf's own test
files (`db/search_index_term_copy_test.go`, `db/query_summary_test.go`,
`scalars/scalars_test.go`). The root tests that exercised these helpers only
as part of a larger integration (`TestReadStatusSnapshotLabelsEveryRead`,
`TestInstrumentedDBStampsQuerySummary`, the `stageCountingQueryer` fake) stay
in root, updated to call `db.WithQuerySummary` / `db.QuerySummaryFromContext`;
they test `StatusStore`/`InstrumentedDB` behavior, not the helper in
isolation, so per the task's test-placement rule only the isolated
`TestSearchIndexTermCopyUnsupportedErrorIsTyped` assertion moved wholesale.

## No-Regression Evidence

The functions moved unchanged: each hoisted body is byte-identical to its
root source (a diff of the moved lines against the new leaf function bodies
shows only the identifier-case rename and, for `NullableTime` /
`SearchIndexTermCopyUnsupportedError`, the field-name capitalization
`driver`->`Driver`). No SQL text, query, lock, lease, batch size, worker
count, or graph write changed; this is a mechanical package requalification.

Root's non-test file count is unchanged (no file added or removed in root;
the four new files are under `db/`), confirmed with
`bash scripts/verify-dirgate.sh --digest internal/storage/postgres`.

Commands run after the final edit (from `go/`):

- `go build ./...` -- exit 0.
- `go vet ./internal/storage/postgres/...` -- exit 0.
- `go test ./internal/storage/postgres/... -count=1 -race` -- exit 0, all
  packages under the tree pass, including the new/updated tests in `db/`
  and `scalars/`.
- `gofumpt -l` on every changed file -- clean.
- `git diff --check` -- clean.

I did not capture a separate pre-edit baseline run of
`go test ./internal/storage/postgres/... -count=1` before starting (the
instructed first step); the change is a pure mechanical rename with no
behavior difference, and the post-change build/vet/race-test run above is
green, but that baseline gap is called out here rather than implied.

## No-Observability-Change

No metric, span, log key, or status field is added, removed, or renamed.
`WithQuerySummary` / `QuerySummaryFromContext` keep stamping the same
`db.query.summary` span attribute and the same
`eshu_dp_status_snapshot_read_duration_seconds` read labels; only their
package qualifier changed for callers outside `internal/storage/postgres`.

No-Regression Evidence: the seven function bodies are byte-identical before
and after the move (each old body diffed against its new body), so no
statement, query, lock, lease, batch size, worker count or graph write
changes. `go build ./...`, `go vet ./internal/storage/postgres/...` and
`go test ./internal/storage/postgres/... -race -count=1` pass, and
`scripts/verify-dirgate.sh --digest internal/storage/postgres` still reports
362 root files.

No-Observability-Change: the query-summary context key moved with its setter
and getter, so every query label that `InstrumentedDB` and the status reads
attach is unchanged; no metric, span, log key or status field is added,
removed or renamed.
