# AGENTS.md - internal/parser/json

## Read first

1. `README.md` - package purpose, ownership boundary, and invariants.
2. `doc.go` - godoc contract for parent parser callers.
3. `language.go` - `Parse`, `Config`, payload setup, and JSON dispatch.
4. `ordered_object.go` - order-preserving top-level and nested object helpers,
   plus `jsonFilenameNeedsOrderedEntries` (the filename routing table between
   the full ordered walk and the cheap top-level-keys-only scan), and the
   real-line-number primitives (`unmarshalOrderedJSONObjectAt`,
   `orderedJSONSectionLines`, `unmarshalOrderedJSONArrayLines`,
   `jsonObjectKeyLines`, `jsonObjectExtractKey`) built on `newline_index.go`.
5. `newline_index.go` - the per-file byte-offset->line binary-search index
   every real `line_number` lookup shares.
6. `lockfile_lines.go` - the lockfile-specific line-lookup helpers
   (`lockfileSectionLines`, `lockfileNestedSectionLines`,
   `lockfileArrayElementLines`) that keep package-lock.json/composer.lock/etc.
   off the full-ordered-walk path (issue #4873) while still reporting real
   per-entry lines (issue #5329).
7. `jsonc.go` - JSONC comment and trailing-comma stripping.
8. `dbt_manifest.go` - dbt manifest payload construction.
9. `data_intelligence.go` and `governance.go` - replay fixture extraction.
10. Parent wrapper in `../json_language.go`.

## Invariants this package enforces

- Do not import the parent `internal/parser` package. The parent wrapper depends
  on this package and supplies parent-owned helpers through `Config`.
- Preserve existing JSON payload bucket names and row fields.
- Preserve document order for metadata, dependency, script, and TypeScript path
  rows when ordered JSON or JSONC data is available.
- Keep CloudFormation extraction delegated to `internal/parser/cloudformation`.
- Keep dbt SQL lineage parsing in the parent package.

## Common changes

- New JSON or JSONC document shapes belong in `language.go` only when they are
  selected by filename or decoded document shape with bounded cost.
- New dbt manifest fields belong in `dbt_manifest.go` and need focused tests
  proving payload rows and coverage state.
- New replay fixture families belong in `data_intelligence.go` unless they are
  governance-specific, where `governance.go` owns the rows.
- New parent-owned behavior should be passed through `Config` instead of adding
  a parent-package import.
- A new filename branch in `Parse`'s switch that reads `topLevelEntries` (like
  `package.json`, `composer.json`, and `tsconfig*.json` today) must also be
  added to `jsonFilenameNeedsOrderedEntries` in `ordered_object.go`, or that
  filename silently falls back to the cheap key-order-only scan and its
  dependency/script rows lose JSON source order.
- A new dependency/script/config row that points at one real JSON source key
  or array element must set `line_number` through the real-position helpers
  (`orderedJSONSectionLines`, `orderedJSONEntryLine`,
  `unmarshalOrderedJSONArrayLines`, or a `lockfile_lines.go` helper for
  lockfile producers), never a `lineNumber := 1; lineNumber++` counter and
  never a hardcoded `1`. A row that summarizes a derived/synthesized record
  with no single source token (see `data_intelligence.go`/`governance.go`)
  must omit `line_number` entirely instead of fabricating one.

## Failure modes

- Missing dependency or script rows usually means ordered-object fallback logic
  drifted in `orderedJSONSectionKeys`.
- Dependency/script/TypeScript-path rows in alphabetical order instead of JSON
  source order usually means `jsonFilenameNeedsOrderedEntries` fell out of
  lockstep with `Parse`'s switch for that filename.
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
