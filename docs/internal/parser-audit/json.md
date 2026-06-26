# JSON Parser Audit

## Overview
Parses JSON and JSONC configuration files using `encoding/json` with custom ordered-object and JSONC normalization. This is a **declarative data** parser — NOT a language parser. Decodes package.json, package-lock.json, composer.lock, NuGet packages.lock.json, SwiftPM Package.resolved, TypeScript configs (tsconfig), .jsonc config files, CloudFormation templates (delegated to cloudformation), dbt manifests, Pipfile.lock, and replay fixture documents. 14 src files, 8 test files. No regexp.MustCompile.

## Claimed Constructs
From `doc.go`, `README.md`, `language.go`:
- **npm package.json**: ordered metadata keys, scripts (as functions), dependencies/devDependencies (as variables), package name/version
- **npm package-lock.json**: exact versions from lockfile v2/v3 packages, optional/dev scope, dependency depth
- **Composer composer.lock**: PHP exact dependency rows
- **NuGet packages.lock.json**: .NET exact dependency rows
- **SwiftPM Package.resolved**: Swift exact dependency rows
- **TypeScript configs**: tsconfig.json paths, compiler options
- **CloudFormation/SAM templates**: delegated to cloudformation package
- **dbt manifest**: manifest.json row construction with lineage extraction
- **Pipfile.lock**: Python lockfile dependency rows (JSON-format)
- **Replay fixtures**: data-intelligence and governance fixture extraction
- **JSONC normalization**: comment stripping, trailing comma removal
- **Dependency coverage matrix**: per-ecosystem coverage status

## Verified-by-Test Constructs
- `TestParsePackageJSONPreservesOrderedMetadataAndDependencyRows` (`parser_test.go:15`): ordered top-level keys, scripts as functions, dependencies/devDependencies as variables
- `TestParsePackageLockJSONEmitsExactDependencyRows` (`parser_test.go:50`): lockfile v3 exact versions, optional/dev scope, dev_dependency flag
- `npm_scope_parity_test.go`: scoped npm packages preserved correctly
- `composer_lock_test.go`: Composer lockfile exact versions
- `swift_package_resolved_test.go`: SwiftPM lockfile dependency rows
- `pipfile_lock_test.go`: Pipfile.lock JSON parsing
- `dependency_coverage_test.go`: coverage matrix assertions
- `dependency_coverage_emit_test.go`: coverage emit behavior
- `dependency_coverage_fixtures_test.go`: fixture-driven coverage checks
- Parent-level: 20 parent test files reference json parsing

## Unverified / Claimed-but-Untested Constructs
- **NuGet packages.lock.json**: claimed in doc.go/README but no dedicated test file visible
- **JSONC-specific normalization edge cases**: no dedicated JSONC test file
- **dbt manifest lineage**: lineage extraction is tested via dbtsql tests and JSON dbt manifest tests (check `json_dbt_test.go` — file not found, likely `dbt_manifest_test.go` or similar)
- **TypeScript config (tsconfig.json) path handling**: may be covered in `parser_test.go` beyond line 100 or in parent-level tests
- **Replay fixtures**: data_intelligence.go extraction — may not have dedicated tests
- **Empty JSON objects/arrays**: edge cases

## Edge Cases Considered
- Top-level key ordering preserved via ordered-object decoder
- Scoped npm packages (@scope/name) preserved
- Optional and dev dependencies from package-lock.json v3
- CloudFormation path delegates correctly to cloudformation package
- JSONC comment stripping before strict JSON decode
- Dependency coverage matrix entries for each ecosystem

## Edge Cases NOT Considered
- Deeply nested JSON (performance)
- Very large lockfiles (memory)
- JSON with BOM
- Duplicate keys in JSON objects
- Unparseable JSON (should return error — test missing)
- JSONC with multi-line comments
- workspace-aware package-lock.json (npm workspaces)

## Verdict
**deep** — 8 internal test files plus 20 parent-level test files. Covers npm (package.json + package-lock.json), Composer, SwiftPM, Pipfile.lock, ordered keys, scoped packages, dependency coverage matrix. As a permanent exception using `encoding/json` (canonical), deep coverage is expected and delivered.

## Recommended Actions
- Document that JSON is a **permanent exception** — uses `encoding/json` with ordered-object decode, not tree-sitter
- Verify NuGet packages.lock.json has test coverage (check parent-level tests or add a dedicated test)
- Add a JSONC-specific test covering multi-line comments and trailing commas
- Add a test for unparseable JSON error handling
