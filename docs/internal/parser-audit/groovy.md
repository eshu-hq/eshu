# Groovy Parser Audit

## Overview
The Groovy parser extracts Jenkins/Groovy pipeline evidence using a hybrid
approach: tree-sitter for syntax entities (classes, methods, imports, calls) and
regex for Jenkins-DSL delivery evidence (shared libraries, pipeline calls, shell
commands, Ansible hints, entry points, configd/pre-deploy flags). Dead-code root
metadata marks Jenkinsfile entrypoints (`pipeline {` / `node {`) and
`vars/*.groovy` shared-library calls. Complexity uses the shared McCabe walker
with Groovy branch nodes. All regex sites are documented permanent exceptions
because the Groovy grammar has no node types for Jenkins-DSL conventions.

## Claimed Constructs
- **Classes** from `class_declaration` nodes (`tree_sitter_syntax.go:49-58`)
- **Methods** from `method_declaration` nodes with bodies; bare no-body pipeline
  steps kept as calls (`tree_sitter_syntax.go:59-76`)
- **Imports** from `import_declaration` nodes with optional alias
  (`tree_sitter_syntax.go:77-89`)
- **Method calls** from `method_invocation` nodes with qualified-name and
  receiver-type metadata (`tree_sitter_syntax.go:90-95`, `groovyInvocationParts`)
- **Shared libraries** via `@Library` annotation and `library` step regex
  (`metadata.go:12-13`)
- **Pipeline calls** via `pipeline*(...)` regex (`metadata.go:14`)
- **Shell commands** via `sh '...'` regex (`metadata.go:15`)
- **Ansible playbook hints** from shell command content (`metadata.go:16`)
- **Entry points** via `entry_point: '...'` regex (`metadata.go:17`)
- **use_configd** boolean via `use_configd: true|false` regex (`metadata.go:18`)
- **pre_deploy** presence via `pre_deploy:` regex (`metadata.go:19`)
- **Jenkinsfile entrypoint** synthetic function root for `pipeline {` / `node {`
  (`entities.go:23`, `parser.go:56-64`)
- **Shared library call** root for `call` in `vars/*.groovy` files
  (`parser.go:50-53`)
- **Cyclomatic complexity** via shared McCabe walker with Groovy branch set
  (`complexity.go:16-29`)
- **PreScan** names from shared_libraries, pipeline_calls, entry_points
  (`parser.go:101-118`)

## Verified-by-Test Constructs
- Classes with names and line numbers:
  `parse_test.go:TestParseExtractsGroovyClassesFunctionsAndCalls` (line 50),
  `parse_test.go:TestParseWithParserExtractsGroovyClassesFunctionsAndCalls` (line 98)
- Methods with `class_context`:
  `parse_test.go:TestParseExtractsGroovyClassesFunctionsAndCalls` (line 87)
- Method calls with `full_name`, `inferred_obj_type`, `lang`:
  `parse_test.go:TestParseWithParserAddsGroovyClassQualifiedCallMetadata` (line 186)
- Lowercase receiver calls left unqualified:
  `parse_test.go:TestParseWithParserKeepsGroovyLowercaseReceiverCallsUnqualified` (line 224)
- Method invocation deduplication:
  `parse_test.go:TestParseWithParserKeepsGroovyMethodInvocationCalls` (line 150)
- Jenkinsfile entrypoint root (`groovy.jenkins_pipeline_entrypoint`):
  `parse_test.go:TestParseMarksJenkinsfileAndSharedLibraryCallAsRoots` (line 262)
- Shared library call root (`groovy.shared_library_call`):
  `parse_test.go:TestParseMarksJenkinsfileAndSharedLibraryCallAsRoots` (line 291)
- Payload identity (path, lang, is_dependency, IndexSource):
  `parse_test.go:TestParseBuildsGroovyPayload` (line 16)
- All nine delivery evidence patterns (shared libraries, pipeline calls, shell
  commands, Ansible hints, entry points, use_configd, pre_deploy):
  `metadata_test.go:TestPipelineMetadataCharacterization` (line 71),
  `metadata_test.go:TestPipelineMetadataExtractsJenkinsSignals` (line 8)
- Metadata.Map preserves parent payload shape:
  `metadata_test.go:TestPipelineMetadataMapPreservesExistingPayloadShape` (line 43)
- PreScan sorted unique metadata names:
  `parse_test.go:TestPreScanReturnsSortedUniqueMetadataNames` (line 321)
- Engine-level dispatch via DefaultEngine.ParsePath:
  `groovy_language_test.go:TestDefaultEngineParsePathGroovyJenkinsfile` (line 11)
- Engine-level Ansible hints:
  `groovy_language_test.go:TestDefaultEngineParsePathGroovyJenkinsfileAnsibleHints` (line 74)
- Engine-level library step with PreScan:
  `groovy_language_test.go:TestDefaultEngineParsePathGroovyJenkinsfileLibraryStep` (line 103)
- Engine-level PreScan:
  `groovy_language_test.go:TestDefaultEnginePreScanPathsGroovyJenkinsfile` (line 138)
- Cyclomatic complexity (straight-line and branchy):
  `engine_cyclomatic_complexity_test.go:TestCyclomaticComplexityPerLanguage` (Groovy cases at line 214)
- Grammar loading via Runtime.Parser:
  `runtime_test.go:TestRuntimeParserLoadsGroovyGrammar` (line 101)
- isSharedLibraryVarsFile path detection:
  `parse_test.go:TestParseMarksJenkinsfileAndSharedLibraryCallAsRoots` (line 316)

## Unverified / Claimed-but-Untested Constructs
- **Import declarations** with `lang` and `alias` fields: claimed in
  `tree_sitter_syntax.go:77-89` and `parser.go:68-69`, but no test asserts
  on the `imports` bucket with specific import names. Existing tests verify the
  bucket is empty or set but not its content.
- **Cyclomatic complexity** on Groovy methods: the shared McCabe walker is
  tested by `engine_cyclomatic_complexity_test.go` with Groovy fixtures, but
  no subdirectory test verifies `groovyCyclomaticComplexity` directly or
  asserts the `cyclomatic_complexity` field on individual Groovy function
  items.
- **`hasGroovyRoot` helper** (`entities.go:45-54`): used in parser.go for
  deduplication but never tested directly.
- **`stringSlice` / `intValue` helpers** (`entities.go:56-81`): tested
  indirectly through bucket assertion helpers in other packages.
- **Error path: nil parser** (`tree_sitter_syntax.go:28-30`): no test provides
  a nil parser to trigger the error case.
- **Error path: nil tree** (`tree_sitter_syntax.go:31-34`): no test causes the
  parser to return a nil tree.
- **PreScanWithParser** (`parser.go:93-99`): no dedicated test; only PreScan is
  tested.
- **Function call ignored names** map (`entities.go:27`): the 19 Jenkins DSL
  keywords that must not become calls are not tested through any fixture that
  exercises each keyword (e.g., `stage`, `steps`, `environment`, `options`).
- **`groovyFunctionCallIgnoredNames` keyword set**: not exhaustively
  characterisation-tested against all entries in the map.

## Edge Cases Considered
- Bare top-level pipeline calls (no-body methods) kept as calls, not function
  entities: `parse_test.go:TestParseBuildsGroovyPayload` (asserts
  `pipelineDeploy` in `function_calls`, empty `functions`)
- Lowercase receiver calls suppressed from qualified inference:
  `parse_test.go:TestParseWithParserKeepsGroovyLowercaseReceiverCallsUnqualified`
- Deduplication within PreScan:
  `parse_test.go:TestPreScanReturnsSortedUniqueMetadataNames` (duplicate
  `pipelineDeploy` + `entry_point`)
- Version-normalization of shared library names (`@v2`, `@main` stripped):
  `metadata_test.go:TestPipelineMetadataCharacterization` (line 99)
- Duplicate shared libraries deduplicated:
  `metadata_test.go:TestPipelineMetadataExtractsJenkinsSignals` (same library via
  annotation and step)
- Jenkinsfile without `pipeline {` / `node {` does not get synthetic root:
  `parse_test.go:TestParseMarksJenkinsfileAndSharedLibraryCallAsRoots`
  (tests that only Jenkinsfile + `pipeline {` produces the root)
- Cyclomatic complexity edge cases (catch/default arms): handled by shared
  walker, tested in `engine_cyclomatic_complexity_test.go`

## Edge Cases NOT Considered
- **Empty Groovy file**: no test for a zero-byte source file
- **Groovy file with syntax errors**: parser behavior on unparseable Groovy
- **Files with only comments/whitespace**: no assertions on empty bucket
  behavior for empty valid files
- **Deeply nested classes**: only single-level class extraction tested
- **Groovy closures** used as call arguments: no test for closures inside
  method invocations
- **Multiple Jenkinsfiles** in a repo: existing tests use a single Jenkinsfile
- **`jenkinsfile` (lowercase) or `jenkinsfile.foo`**: `isJenkinsfile` handles
  case-insensitively but only tested with `Jenkinsfile`
- **All 19 ignored function call names**: no exhaustive keyword-suppression test
- **Null decorators list** on class/function items: not tested
- **PreScanWithParser**: no test at all
- **Metadata.Map nil UseConfigd handling**: tested indirectly via
  `TestPipelineMetadataMapPreservesExistingPayloadShape` but not with nil
  value explicit assertion

## Verdict
**moderate**

The Groovy parser has focused test coverage for its two delivery-evidence
boundaries (tree-sitter syntax and Jenkins-DSL regex) with 15 tests across 3
files plus engine-level integration tests. However, import extraction, error
paths, keyword suppression, and PreScanWithParser are untested, and the test
suite focuses on happy-path Jenkinsfile scenarios without negative/syntax-error
edge cases.

## Recommended Actions
1. Add a test asserting `imports` bucket content from a Groovy file with
   `import` declarations, including the `lang` and `alias` fields.
2. Add an exhaustive characterization test for all 19 entries in
   `groovyFunctionCallIgnoredNames` that proves each keyword is suppressed as
   a function call.
3. Add a test for `PreScanWithParser` matching the contract of the existing
   `PreScan` test.
4. Add tests for error paths: nil parser, nil tree, unreadable file.
5. Add a test for empty/syntax-error Groovy files to prove deterministic empty
   buckets are returned.
6. Add a test verifying `cyclomatic_complexity` field appears on Groovy function
   items with the expected value.
