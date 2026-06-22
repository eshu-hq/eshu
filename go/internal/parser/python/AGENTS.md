# AGENTS.md - internal/parser/python guidance

## Read first

1. README.md - package boundary, parser surface, and invariants
2. doc.go - godoc contract for the Python adapter package
3. language.go - Parse, PreScan, payload bucket assembly, and tree-sitter walk
4. shared_helpers.go - allowed shared helper imports and copied wire-shape helpers
5. notebook.go and notebook_temp.go - notebook extraction and temporary source file lifecycle
6. dead_code_roots.go, lambda_roots.go, public_api_roots.go - dead-code root evidence
7. framework_routes.go, ast_nodes.go - FastAPI/Flask route and ORM table
   semantics plus the reusable decorator/call/argument AST node helpers
9. semantics.go, imports.go, call_inference.go, annotation_support.go - metadata helpers
10. cfg_emit.go - opt-in value-flow buckets (EmitDataflow) over python/pydataflow
11. payload_buckets.go - sortNamedBucket / collectBucketNames bucket utilities
12. notebook_test.go and language_test.go - child package contract coverage

## Invariants this package enforces

- Dependency direction stays one way: the parent parser package may import this
  package, but this package must not import internal/parser.
- Parse receives a caller-owned tree-sitter parser and must not close it.
- NotebookSource only keeps executable code cells. Markdown, raw, malformed,
  and blank cells do not become parser input.
- Invalid notebook JSON returns an error instead of partial source.
- SAM and serverless Lambda roots only mark handlers when config declares a
  Python runtime.
- Parent Engine signatures stay in go/internal/parser/python_language.go.

## Common changes and how to scope them

- Add parse behavior with a failing test in language_test.go or a parent
  engine_python_* test before editing adapter code.
- Add notebook behavior with a focused test in notebook_test.go first.
- Keep generic parser helpers in the shared parser package when multiple
  language adapters need them.
- Keep Python-only evidence here instead of adding new parent helper branches.

## Failure modes and how to debug

- Missing notebook functions or classes usually means code cells were skipped
  or cell source was joined incorrectly. Reproduce the notebook shape in
  notebook_test.go.
- Missing Lambda roots usually means config discovery did not walk to the repo
  root, YAML templating was not sanitized, or the handler path did not resolve
  to the parsed source file.
- Missing call receiver metadata usually starts in call_inference.go; check
  class context and constructor assignment scope before changing payload shape.
- Missing public API roots usually starts in public_api_roots.go; compare the
  package __init__.py import form to the parsed source path.

## Anti-patterns specific to this package

- Importing the parent parser package to reuse unexported helpers.
- Closing the tree-sitter parser passed to Parse.
- Treating markdown or raw notebook cells as Python code.
- Marking non-Python SAM or serverless handlers as Python dead-code roots.
- Moving registry dispatch or Engine method signatures into this package.

## What NOT to change without an ADR

- Do not change parser payload bucket names or wire fields without updating the
  query and docs contract.
- Do not make Python Lambda config discovery unbounded outside the repository
  root.
- Do not add cross-language helper dependencies here when the shared parser
  package is the correct boundary.

## Evidence notes

### Framework/route/ORM/dead-code/public-api regex to AST (issue #3538)

FastAPI/Flask route semantics, SQLAlchemy/Django ORM table mappings, the
script-main guard, the dunder-protocol install evidence, and the `__all__`
public-API export extraction now read tree-sitter AST nodes (decorator, call,
keyword_argument, assignment, type_alias_statement, class_definition, list, and
tuple) instead of `(?m)` regex over source text. The `(?m)` route/ORM regex
block in `semantics.go`, the text-offset handler scan in
`python_route_handlers.go` (file deleted), the main-guard and dunder line-scan
regex in `dead_code_roots.go`, and the `__all__` literal regex in
`public_api_roots.go` are removed. The former `regexp` usage in `language.go`
(class header, function signature) and `embedded_shell.go` (subprocess/os import
evidence) is now also on AST in this branch under epic #3531 — see the
within-node section below. No `regexp` import remains in the package.

No-Regression Evidence: `go test ./internal/parser/... -count=1` stays green
(1112 baseline plus the new AST-node tests in
`internal/parser/python/semantics_test.go`). The pre-existing engine parity
suites still pass byte-for-byte:
`go test ./internal/parser -run 'TestDefaultEngineParsePathPython(FastAPISemantics|FlaskSemantics|ORMMappings|UnknownRouteDecoratorRemainsUnclassified|FastAPIBindsDefHandler|FlaskBindsDefHandler|EmitsScriptMainGuardRoot|EmitsReversedScriptMainGuardRoot|DunderAssignmentEvidenceIsEnclosingScopeScoped|EmitsPublicAPIRootKinds)' -count=1`.
The orphan route (`@app.post("/orphan")` with no following def) still emits the
route with no handler because tree-sitter parks the bare decorator under an
ERROR node whose parent is not a `decorated_definition`, preserving the #2788
correlation-truth contract.

No-Regression Evidence (PR #3544 Codex P2 follow-up): the AST `__all__` reader
now descends concatenated literals. `__all__ = ["foo"] + ["bar"]` parses with a
`binary_operator` right-hand side, so `pythonStringSequenceLiterals` recurses
through both operands (and nested `binary_operator` nodes) and collects string
literals from each `list`/`tuple`/`set` operand. Before the fix the helper
returned no names for the split form and the exported symbols lost their
`python.module_all_export` roots, surfacing as dead-code candidates; the old
regex RHS scan had captured them.
`go test ./internal/parser -run 'TestDefaultEngineParsePathPythonConcatenatedAllExportsAreRoots' -count=1`
and `go test ./internal/parser/python -run TestPythonModuleAllNamesAcceptsConcatenatedLiterals -count=1`
fail before the fix and pass after, while the plain list/tuple `__all__` and
`TestDefaultEngineParsePathPythonEmitsPublicAPIRootKinds` suites stay green. The
augmented form `__all__ += [...]` parses as `augmented_assignment`, a node kind
the prior regex-on-`assignment` path never captured, so it stays out of scope to
preserve parity.

No-Observability-Change: this change is parser-internal; it edits only how the
existing `framework_semantics`, `orm_table_mappings`, and
`dead_code_root_kinds` payload fields are computed. No metric instrument, metric
label, span, log line, status field, env var, queue, worker, lease, batch,
runtime knob, or graph query is added or changed. Operators still diagnose
parser behavior through existing collector parse-stage logs and
`eshu_dp_file_parse_duration_seconds`.

### Function-signature and class-header within-node regex to AST (epic #3531)

The last three within-node `regexp` sites now read tree-sitter nodes instead of
matching the rendered node text:

- `pythonTypeAnnotations` (`annotation_support.go`) walks the
  `function_definition` `parameters` and `return_type` fields. Only
  `typed_parameter`/`typed_default_parameter` nodes (those carrying a `type`
  field) and a present `return_type` produce `type_annotations` entries; splat
  parameters unwrap their `list_splat_pattern`/`dictionary_splat_pattern` to the
  bare identifier name, matching the prior `*`/`**` stripping.
- `pythonClassBaseNames` (`class_context.go`) and `pythonClassMetaclass`
  (`language.go`) read the `class_definition` `superclasses` argument list:
  positional arguments become `bases` (trailing dotted name, deduped, sorted),
  and the `metaclass=` `keyword_argument` value becomes `metaclass`. Reading the
  AST means multi-line class headers are captured the same as single-line ones,
  which the prior single-line `(?m)` regex could not match.
- `embedded_shell.go` resolves `embedded_shell_commands` from `call` nodes:
  subprocess/os import aliases come from `import_statement`/
  `import_from_statement` nodes, a call is attributed to its outermost enclosing
  `function_definition` (module-level calls are not reported), and a call is
  dropped when its alias was rebound by an earlier plain `assignment` in that
  function. Comments and string literals are never `call` nodes, so the
  comment/string-suppression behavior is intrinsic rather than a scrub pass.
  The deleted `pythonFunctionSignatureRe`, `pythonClassHeaderRe`,
  `pythonFunctionSignature`, `splitPythonParameters`,
  `parsePythonParameterAnnotation`, and all line-scan regexes leave the package
  with no `regexp` import.

No-Regression Evidence: `go test ./internal/parser/python ./internal/parser
-count=1` (652 tests) and `go test ./internal/parser
./internal/collector/discovery ./internal/content/shape ./internal/collector
-count=1` (1020 tests) stay green; `golangci-lint run ./internal/parser/...`
reports no issues. The pre-existing engine parity suites pass byte-for-byte:
`go test ./internal/parser -run 'TestDefaultEngineParsePathPython(EmitsTypeAnnotationsBucket|EmitsMetaclassMetadata|EmbeddedShellCommands)' -count=1`.
New guards added in `internal/parser/engine_python_ast_parity_test.go`:
`TestDefaultEngineParsePathPythonMultilineClassHeaderUsesAST` fails on the prior
single-line regex and passes on AST; the splat-typed-parameter and rich
embedded-shell (module alias, direct import, os module, alias shadowing,
nested-function attribution, module-level skip) tests lock the payload
byte-for-byte against the prior line-scan output.

No-Observability-Change: parser-internal; it changes only how the existing
`type_annotations`, `classes[].bases`, `classes[].metaclass`, and
`embedded_shell_commands` payload fields are computed. No metric instrument,
metric label, span, log line, status field, env var, queue, worker, lease,
batch, runtime knob, or graph query is added or changed. Operators still
diagnose parser behavior through existing collector parse-stage logs and
`eshu_dp_file_parse_duration_seconds`.
