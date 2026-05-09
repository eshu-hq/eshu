# AGENTS.md - internal/parser/json

## Read first

1. `README.md` - package purpose, ownership boundary, and invariants.
2. `doc.go` - godoc contract for parent parser callers.
3. `language.go` - `Parse`, `Config`, payload setup, and JSON dispatch.
4. `ordered_object.go` - order-preserving top-level and nested object helpers.
5. `dbt_manifest.go` - dbt manifest payload construction.
6. `data_intelligence.go` and `governance.go` - replay fixture extraction.
7. Parent wrapper in `../json_language.go`.

## Invariants this package enforces

- Do not import the parent `internal/parser` package. The parent wrapper depends
  on this package and supplies parent-owned helpers through `Config`.
- Preserve existing JSON payload bucket names and row fields.
- Preserve document order for metadata, dependency, script, and TypeScript path
  rows when ordered JSON data is available.
- Keep CloudFormation extraction delegated to `internal/parser/cloudformation`.
- Keep dbt SQL lineage parsing in the parent package.

## Common changes

- New JSON document shapes belong in `language.go` only when they are selected
  by filename or decoded document shape with bounded cost.
- New dbt manifest fields belong in `dbt_manifest.go` and need focused tests
  proving payload rows and coverage state.
- New replay fixture families belong in `data_intelligence.go` unless they are
  governance-specific, where `governance.go` owns the rows.
- New parent-owned behavior should be passed through `Config` instead of adding
  a parent-package import.

## Failure modes

- Missing dependency or script rows usually means ordered-object fallback logic
  drifted in `orderedJSONSectionKeys`.
- Missing CloudFormation rows usually means `cloudformation.IsTemplate` did not
  recognize the decoded document shape.
- Missing dbt column lineage usually means the parent wrapper did not supply
  `LineageExtractor` or the manifest lacked compiled model SQL.
- Flaky payload ordering usually means a map iteration path was added without a
  deterministic sort.

## What not to change without an ADR

- Do not make this package read repository state beyond the single file passed
  to `Parse`.
- Do not add graph, collector, storage, query, projector, or reducer
  dependencies.
- Do not move `dbt_sql_lineage.go` or its parent-exported lineage types into
  this package.
