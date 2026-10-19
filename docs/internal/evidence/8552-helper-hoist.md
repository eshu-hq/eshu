# Evidence: tfstate helper hoist to filters + fixtures leaves (#8552)

## Change

New `go/internal/storage/postgres/filters` (production `CleanFactKinds`)
and `go/internal/storage/postgres/fixtures` (test-only
`ConfigParserRow`/`ConfigResourcesArray`/`PadQueueFakeRow`),
each with `doc.go`, `README.md`, `AGENTS.md`, and behavior tests. Root and
tfstate call sites rewired; `runtime_drift_config_fixtures_test.go` and
`work_queue_fake_row_padding_test.go` deleted. Two single-word packages
instead of one `facthelpers` leaf per the naming-glue gate. No SQL, DDL,
queue, lease, or graph change: every moved body is byte-identical to the
copy it replaces (verified by diff before deletion).

## No-Regression Evidence: behavior identical, single source

- `go test -count=1 ./internal/storage/postgres/` green (full package,
  includes the rewired AWS/multi-cloud drift and queue-lifecycle tests).
- `go test -count=1 ./internal/storage/postgres/tfstate/` green.
- New `filters`/`fixtures` package tests pin trim/dedup/order, fixture
  JSON shape, and padding widths, so a mutation that changes a helper
  breaks a test (acceptance criterion 2).
- Case-sensitive repo search confirms no lowercase `cleanFactKinds`,
  `padQueueFakeRow`, `fixtureConfigParserRow`, or
  `fixtureConfigResourcesArray` definitions remain outside the leaves
  (acceptance criterion 1).

## No-Observability-Change: no signals touched

No new spans, metrics, logs, routes, workers, or knobs. Moved code runs
in identical call positions with identical arguments.
