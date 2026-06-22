# AGENTS.md - internal/parser/groovy guidance

## Read first

1. README.md - package boundary, metadata fields, and invariants
2. doc.go - godoc contract for the Groovy helper package
3. tree_sitter_syntax.go - tree-sitter syntax extraction for classes,
   methods, imports, and calls
4. metadata.go - Jenkins/Groovy regex extraction and payload compatibility map
5. metadata_test.go - behavior coverage for delivery metadata and map shape

## Invariants this package enforces

- Dependency direction stays one way: parent parser code may import this
  package, but this package must not import internal/parser.
- Class, method, import, and call entities come from the Groovy tree-sitter
  grammar. Jenkins delivery metadata remains bounded lexical evidence.
- PipelineMetadata returns typed evidence; Metadata.Map is the compatibility
  bridge for existing payload consumers.
- Shared library versions are normalized away before returning metadata.
- Bare top-level pipeline steps can parse as no-body method declarations; keep
  them as call evidence, not fake function entities.

## Common changes and how to scope them

- Add Jenkins/Groovy evidence by writing a focused test in metadata_test.go
  first.
- Keep file reading and pre-scan wiring in the parent parser package.
- Keep query-specific enrichment out of this package.

## Failure modes and how to debug

- Missing shared libraries usually means the annotation or library step regex
  did not match the Jenkinsfile form.
- Missing class, method, import, or call rows usually means tree-sitter node
  handling drifted from the grammar's current node types.
- Missing Ansible hints usually means the shell command was not extracted
  before ansible-playbook matching.
- Query regressions usually mean Metadata.Map drifted from the legacy payload
  shape.

## Anti-patterns specific to this package

- Importing the parent parser package to reuse payload helpers.
- Returning only map[string]any and losing the typed helper contract.
- Adding repository-specific Jenkins conventions without fixture evidence.

## What NOT to change without an ADR

- Do not move query or relationship interpretation into this package. It owns
  parser evidence only.

## Regex-to-AST Audit — Permanent Exceptions (issue #3575, epic #3531)

This section documents the result of the within-node regex audit for this
package.  Every regex site was reviewed against the Groovy tree-sitter grammar
and against the AST extraction already present in `tree_sitter_syntax.go`.

### Design boundary

`tree_sitter_syntax.go` uses the Groovy tree-sitter grammar to extract genuine
syntax entities — `class_declaration`, `method_declaration`,
`import_declaration`, and `method_invocation` nodes — as structured entity
rows.  That is the correct home for any extraction that the grammar models as a
distinct node type.

All nine patterns below operate on source text **after** the tree-sitter parse
and recover Jenkins-DSL delivery evidence that the grammar does not model as
node types.  They are templated detection / content classification over raw
source text, not symbol extraction.  None of them should be migrated to AST
traversal.

### Pattern inventory

| Pattern variable | File | Category | Justification |
|---|---|---|---|
| `groovyLibraryPattern` | `metadata.go` | Templated detection / delivery evidence | Matches `@Library('name')` annotations. The Groovy grammar parses `@Library(...)` as an annotation node but does not expose the string argument as a named field that identifies a shared-library reference. Extracting the library name requires matching the annotation's argument text. |
| `groovyLibraryStepPattern` | `metadata.go` | Templated detection / delivery evidence | Matches `library identifier: 'name'` step calls. The grammar records this as a `method_invocation`, but the library name is embedded in a named-argument string value — the `method_invocation` node carries no field that marks the identifier as a shared-library reference. Content classification over source text is the correct approach. |
| `groovyPipelineCallPattern` | `metadata.go` | Templated detection / delivery evidence | Matches pipeline-step call names of the form `pipelineXxx(`. These are Groovy method calls that the grammar does model as `method_invocation`, but they are captured here as **delivery evidence** (which pipeline step pattern is in use), not as general function-call entities. The AST layer already records them in `calls`; this pattern captures them as a typed `PipelineCalls` evidence field consumed by downstream query logic that cares specifically about pipeline step identity. |
| `groovyShellCommandPattern` | `metadata.go` | Templated detection / delivery evidence | Matches `sh 'command'` arguments. The grammar records `sh(...)` as a method invocation but does not expose the argument string as a shell command. The command text is the delivery evidence; the grammar provides no shell-command-typed node. |
| `groovyAnsiblePattern` | `metadata.go` | Content classification / delivery evidence | Applied to shell command strings already extracted by `groovyShellCommandPattern`. Detects `ansible-playbook` invocations and captures playbook path and optional inventory flag. This is classification over text content, not source-level syntax — there is no tree-sitter node type for ansible invocations. |
| `groovyEntryPointPattern` | `metadata.go` | Templated detection / delivery evidence | Matches `entry_point: 'value'` named arguments inside pipeline step calls. The grammar records named arguments as key-value pairs inside an `argument_list`, but it does not expose a field named `entry_point` as a semantic concept. Matching the key name in source text is the correct approach for this Jenkins convention. |
| `groovyUseConfigdPattern` | `metadata.go` | Templated detection / delivery evidence | Matches `use_configd: true\|false`. Same rationale as `groovyEntryPointPattern`: named argument with a Jenkins-convention key that carries no semantic type in the grammar. |
| `groovyPreDeployPattern` | `metadata.go` | Templated detection / delivery evidence | Detects presence of a `pre_deploy:` key. Boolean presence flag — the grammar offers no `pre_deploy` node. Presence/absence is the evidence; no AST alternative exists. |
| `groovyJenkinsEntrypointPattern` | `entities.go` | Templated detection / DSL block opener | Detects `pipeline {` / `node {` at the start of a line to attach a synthetic Jenkinsfile entrypoint root entity. These block openers are Jenkins DSL idioms; the Groovy grammar parses them as method calls or block closures without exposing a dedicated pipeline-root or node-root node type. This is already documented inline in `entities.go`. |

### Why migration is not applicable

The Groovy tree-sitter grammar does not define node types for:
shared-library annotations, pipeline-step identity, shell command arguments,
ansible invocations, named convention arguments (`entry_point`, `use_configd`,
`pre_deploy`), or Jenkins DSL block openers.  There is therefore no AST node
to migrate to.  Forcing these patterns into AST traversal would require
inventing ad-hoc node-text matching inside a grammar walk, which is identical
in cost and correctness risk to the current regex approach while obscuring the
intent.

These nine sites are **permanent exceptions** to the regex-to-AST migration
policy for this package.  Any future addition of a Jenkins-convention evidence
field should follow the same pattern: regex over source text, with a
characterization test in `metadata_test.go` that pins the output before the
pattern is merged.

### No-Regression Evidence

```
cd go && gofmt -l internal/parser/groovy   # empty — no formatting violations
cd go && go test ./internal/parser/... -count=1
# 1171 passed in 41 packages (1170 before this issue; +1 is TestPipelineMetadataCharacterization)
cd go && golangci-lint run ./internal/parser/...
```

Run and verified locally.  All gates passed.

### No-Observability-Change

This issue adds documentation and a characterization test only.  No runtime
code paths, spans, metrics, or log lines were added or removed.  No
observability change.
