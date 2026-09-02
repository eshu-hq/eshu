# Groovy Parser Helpers

## Purpose

This package owns Jenkins/Groovy parser extraction that does not need parent
parser internals. It builds the Groovy payload, pre-scan names, syntax entities,
and delivery metadata for shared libraries, pipeline calls, shell commands,
Ansible playbook hints, entry points, and configd/pre-deploy flags. Class,
method, import, and call entities are extracted from the Groovy tree-sitter
grammar so Groovy repositories can participate in code search and dead-code
candidate reads without the old regex declaration scanner.

## Ownership boundary

The package is responsible for Groovy parse and pre-scan behavior plus typed
delivery evidence. The parent parser package still owns parser registry
dispatch, content metadata enrichment, and the exported compatibility wrapper
used by query and relationship code.

## Exported surface

The godoc contract is in doc.go. Current exports are Metadata,
AnsiblePlaybookHint, Parse, ParseWithParser, PreScan, PreScanWithParser,
PipelineMetadata, and Metadata.Map. Metadata carries SharedLibraries,
PipelineCalls, ShellCommands, AnsiblePlaybookHints, EntryPoints, UseConfigd,
and HasPreDeploy.

The lexical `ExtractClassEntities`, `ExtractFunctionEntities`, and
`ExtractFunctionCallEntities` extractors were removed (issue #3540): class,
method, and call entities are produced solely by the tree-sitter syntax index in
`tree_sitter_syntax.go`, so the regex declaration scanner had no remaining
production caller.

## Dependencies

This package imports `internal/parser/shared`, `go-tree-sitter`, and the Groovy
tree-sitter grammar binding. It must not import the parent parser package,
collector packages, graph storage, or reducer code.

## Engine test surface

`groovy_language_test.go` and `groovy_jenkins_golden_fixture_test.go` (external
package `groovy_test`, relocated from the parent by #6062) drive
`parser.DefaultEngine().ParsePath` and `PreScanPaths` against Jenkinsfile and
golden-corpus fixtures: 7 test functions, from
`rg --no-filename -o '^func Test' groovy_language_test.go groovy_jenkins_golden_fixture_test.go | wc -l`.
They may import `internal/parser` because they sit in the external `groovy_test`
package: an external test package is compiled separately from `internal/parser`
itself, so the import does not close the cycle that `parsertest` would otherwise
create. Keep that exception limited to black-box tests of the public parent
engine. Shared
assertions come from `go/internal/parser/parsertest`;
`groovy_test_helpers_test.go` holds only `groovyFixturePath`,
`writeGroovyTestFile` (creates parent directories before delegating to
`parsertest.WriteFile` for the nested `src/` fixture), and a local
`assertEmptyNamedBucket` copy because parsertest has no empty-bucket assertion.
The 4 `package groovy` `*_test.go` files (18 test functions, from
`rg --no-filename -o '^func Test' parse_test.go metadata_test.go pipeline_metadata_gate_test.go equivalence_dump_test.go | wc -l`)
stay white-box and must not import the parent.

Run the engine tests from the `go/` module root with
`go test ./internal/parser/groovy -count=1`; pin a subset with
`../scripts/go-test-run-guard.sh 5 TestDefaultEngineParsePathGroovy -- ./internal/parser/groovy -count=1`,
which runs from the `go/` module root and fails closed if fewer than 5 tests
match (minimum derived from
`go test -list 'TestDefaultEngineParsePathGroovy' ./internal/parser/groovy`).

## Telemetry

This package emits no telemetry. Parse timing remains owned by the parent
parser engine.

## Gotchas / invariants

PipelineMetadata normalizes shared library versions such as `pipelines@v2`
down to `pipelines`, matching existing parser payload behavior.

Tree-sitter extraction owns class, method, import, and method-call entities.
Class-qualified method calls carry `full_name`, `inferred_obj_type`, and `lang`
metadata so reducer-owned code-call resolution can bind `Helper.run()` to the
right class method without falling back to weak repository-wide name matching.
Bare top-level pipeline calls can parse as no-body method declarations in the
Groovy grammar; the parser keeps those as call rows and does not promote them to
function entities. Metadata extraction remains lexical because Jenkins shared
library annotations, shell strings, and Ansible hints are bounded delivery
evidence rather than Groovy syntax ownership.

`ParseWithParser` marks declarative/scripted Jenkinsfiles with
`groovy.jenkins_pipeline_entrypoint` when a top-level `pipeline {` or `node {`
block is present, even when the file also declares helper functions. It marks
`call` in `vars/*.groovy` shared-library files with
`groovy.shared_library_call` for absolute and repository-relative paths. Those
root kinds are metadata only; the parser does not resolve Groovy dynamic
dispatch, closure delegates, or Jenkins shared library loading.

### Within-AST / source-text regex exceptions (issue #3540)

These retained regexes do not perform primary symbol or edge extraction, so they
are documented exceptions rather than AST conversions:

- `metadata.go` patterns (`@Library`, `library`, `pipeline*(`, `sh '...'`,
  `ansible-playbook`, `entry_point:`, `use_configd:`, `pre_deploy:`) extract
  Jenkins/Groovy delivery evidence from inside Groovy string literals and shell
  command strings. Shell commands and Jenkins annotation arguments are not Groovy
  syntax nodes, so there is no AST node to walk; the regex matches bounded
  delivery hints, and Groovy metaprogramming stays a named exactness blocker.
- `groovyJenkinsEntrypointPattern` (`entities.go`) detects the top-level
  `pipeline {` / `node {` DSL idiom to attach a synthetic Jenkinsfile entrypoint
  root. The opener is a DSL convention, not a distinct grammar node, so it is
  matched on source text after the tree-sitter parse.

Parse and PreScan use shared payload helpers so bucket shape and pre-scan
ordering stay aligned with other language-owned parser packages. Metadata.Map
keeps the legacy `map[string]any` payload shape because query and relationship
packages still consume `parser.ExtractGroovyPipelineMetadata`.

## Related docs

- docs/public/languages/support-maturity.md
