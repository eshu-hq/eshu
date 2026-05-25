# JSON Parser

## Purpose

`internal/parser/json` owns JSON file parsing for the parent parser engine. It
decodes JSON, `.jsonc`, and JSONC-compatible TypeScript config files, preserves
top-level document order for metadata buckets, and emits the legacy payload
rows consumed by collector and projection code.

## Ownership boundary

This package owns JSON decoding, JSON-specific ordered-object handling,
package-manager manifest rows, npm `package-lock.json` and Composer
`composer.lock` exact dependency rows, TypeScript config rows, dbt
manifest payload construction, and data-intelligence replay fixture
extraction. The replay code is split across
domain files so no single helper becomes a catch-all parser. This package does
not own parser dispatch, repository discovery, fact persistence, graph
projection, YAML decoding, or dbt SQL lineage parsing.

## Exported surface

The godoc contract is in `doc.go`. Current exports are:

- `Config` carries parent-owned helpers needed without importing the parent
  parser package.
- `LineageExtractor` supplies compiled dbt SQL lineage to manifest parsing.
- `ColumnLineage` and `CompiledModelLineage` mirror the parent lineage result
  shape at this package boundary.
- `Parse` returns one JSON parser payload for a file path.
- `DependencyCoverageStatus`, `DependencyCoverageEntry`, and
  `DependencyCoverage` publish the repository dependency parser coverage
  matrix that feeds the supply-chain impact reducer. `DependencyCoverageByFile`
  looks up a single entry by lowercase filename. The matrix is the in-code
  source of truth behind
  [`docs/public/reference/dependency-coverage.md`](../../../../docs/public/reference/dependency-coverage.md);
  guard tests in `dependency_coverage_test.go` keep it aligned with what
  `Parse` actually emits.

## Dependencies

This package imports `internal/parser/shared` for `Options`, `BasePayload`, and
`ReadSource`. It imports `internal/parser/cloudformation` so JSON templates use
the same CloudFormation and SAM extraction as YAML. It must not import
`internal/parser`, collector, storage, query, projector, or reducer packages.

## Telemetry

This package emits no metrics, spans, or logs. Parser timing and failures remain
owned by the collector snapshot path and parent engine callers.

## Gotchas / invariants

JSON object order matters for `json_metadata.top_level_keys`, dependency rows,
script rows, and TypeScript `compilerOptions.paths` rows. Keep ordered-object
helpers in this package and use sorted fallback paths when decoded maps lose
order. JSONC normalization strips comments and trailing commas for `.jsonc`
files and TypeScript config files before decoding. Trailing-comma removal uses
bounded byte lookahead so large config files do not pay repeated substring
trims.

`package-lock.json` rows represent exact npm versions installed by the owning
repository. `package.json` dependency rows can still contain ranges such as
`^5.4.11`; callers that need observed package versions must prefer lockfile
rows and keep manifest ranges as partial evidence.

`composer.lock` rows likewise represent exact PHP package versions
installed by Composer. The parser emits one row per package in the
`packages` (runtime) and `packages-dev` (dev) arrays, preserves the
`vendor/name` identity, sets `package_manager: "composer"` and
`lockfile: true`, and does not yet derive a transitive dependency chain.
Composer manifest ranges from `composer.json` stay in their own
`require`/`require-dev` rows so downstream code can present both the
declared range and the installed version as joined evidence.

dbt SQL lineage stays parent-owned. Do not import `internal/parser` from this
package; add only narrow callback fields to `Config` when parent-owned behavior
must be supplied. The parent wrapper converts the lineage result into the JSON
package boundary type.

CloudFormation and SAM documents return after template extraction so generic
JSON dependency rows do not mix with infrastructure payload rows.

## Related docs

- `docs/public/languages/support-maturity.md`
- `docs/public/reference/dependency-coverage.md`
- `docs/public/reference/security-intelligence.md`
