# Python Parser Audit

## Overview
The Python parser (`go/internal/parser/python/`) is the most thoroughly tested language adapter in Eshu. It parses `.py` and `.ipynb` files via tree-sitter, emitting functions, classes, modules, variables, imports, calls, type annotations, framework semantics (FastAPI/Flask), ORM table mappings, embedded shell commands, dead-code root evidence, generator flags, rationale comments, property/cached-property/setter decorator metadata, and opt-in value-flow buckets. The package has 4 subdirectory test files plus 27 parent-level engine-python test files.

## Claimed Constructs
List every construct the parser claims to extract, with source references.

1. **Classes** — `language.go:75-109` (`class_definition`)
2. **Functions** — `language.go:110-174` (`function_definition`)
3. **Lambda functions** — `language.go:176,235-238` (assignment-derived and anonymous)
4. **Modules** — `language.go:50,59-70` (`module` with docstring)
5. **Variables** — `language.go:175-202` (`assignment`, module-scoped or all-scope)
6. **Imports** — `language.go:203-210`, `imports.go:14-101` (`import_statement`, `import_from_statement`)
7. **Function calls** — `language.go:211-234` (`call`)
8. **Type annotations** — `language.go:172-174`, `annotation_support.go:18-59` (parameter/return annotations)
9. **Annotated assignment annotations** — `annotation_support.go:130-161`
10. **Framework semantics** (FastAPI, Flask routes) — `framework_routes.go:42-58`, `language.go:249`
11. **ORM table mappings** (SQLAlchemy, Django) — `framework_routes.go:309-430`, `language.go:250`
12. **Dead-code root kinds** (`dead_code_roots.go:134-170`, `language.go:132-158`):
    - `python.fastapi_route_decorator`
    - `python.flask_route_decorator`
    - `python.celery_task_decorator`
    - `python.click_command_decorator`
    - `python.typer_callback_decorator`
    - `python.typer_command_decorator`
    - `python.property_decorator`
    - `python.dataclass_model`
    - `python.script_main_guard`
    - `python.aws_lambda_handler`
    - `python.dataclass_post_init`
    - `python.dunder_method`
    - `python.public_api_member`
    - `python.module_all_export`
    - `python.package_init_export`
    - `python.public_api_base`
    - Class protocol method names (128 entries) — `dead_code_roots.go:23-127`
    - Module protocol function names (2 entries) — `dead_code_roots.go:129-132`
13. **Embedded shell commands** — `embedded_shell.go:34-101` (subprocess/os calls)
14. **Generator detection** — `generator_support.go:8-38` (`semantic_kind: generator`)
15. **Call inference** — `call_inference.go:13-35` (`inferred_obj_type`)
16. **Rationale comments** — `rationale.go:22-56` (# WHY:/# HACK:/# NOTE:/# TODO:/# FIXME:)
17. **Cyclomatic complexity** — `semantics.go:101-103`
18. **Notebook (.ipynb) extraction** — `notebook.go:16-43`, `notebook_temp.go:11-29`
19. **Lambda handler roots** — `lambda_roots.go:28-50` (SAM/serverless config)
20. **Docstrings** — `semantics.go:13-48` (module, class, and function)
21. **Decorators** — `language.go:307-331`
22. **Class context** — `class_context.go:13-24`
23. **Class metaclass** — `language.go:342-359`
24. **Value-flow** (opt-in) — `cfg_emit.go:21-38` (`dataflow_functions`, `taint_findings`, `interproc_findings`)
25. **PreScan** — `language.go:258-265`

## Verified-by-Test Constructs
List constructs verified by tests, with file:function references.

1. **Classes, functions, imports, calls** — `engine_test.go:11-48` (`TestDefaultEngineParsePathPython`)
2. **Module docstring** — `engine_python_module_semantics_test.go:11-38` (`TestDefaultEngineParsePathPythonModuleDocstringEmitsModuleMetadata`)
3. **FastAPI semantics** — `engine_python_semantics_test.go` (FastAPIBindsDefHandler, FastAPISemantics)
4. **Flask semantics** — `engine_python_semantics_test.go` (FlaskBindsDefHandler, FlaskSemantics)
5. **ORM mappings** — `engine_python_semantics_test.go` (ORMMappings, UnknownRouteDecoratorRemainsUnclassified)
6. **Dead-code root kinds (FastAPI/Flask/Celery decorators)** — `python_dead_code_roots_test.go:11-87` (`TestDefaultEngineParsePathPythonEmitsDeadCodeRootKinds`)
7. **CLI root kinds (click, typer)** — `python_dead_code_roots_test.go:91-157` (`TestDefaultEngineParsePathPythonEmitsDeadCodeCLIRootKinds`)
8. **Script main guard root** — `python_dead_code_roots_test.go:159-232` (`TestDefaultEngineParsePathPythonEmitsScriptMainGuardRoot`, `ReversedScriptMainGuardRoot`, `ScriptMainGuardSkipsElseAndNestedDefinitions`)
9. **Dunder method root evidence** — `python_dead_code_roots_test.go:292-337` (`DunderAssignmentEvidenceIsEnclosingScopeScoped`)
10. **AWS Lambda handler root** — `python_dead_code_roots_test.go:340-387` (`EmitsSAMHandlerDeadCodeRootKind`)
11. **Public API roots (all_export, init_export, class member, base)** — `python_dead_code_roots_test.go:408-479` (`TestDefaultEngineParsePathPythonEmitsPublicAPIRootKinds`)
12. **Concatenated `__all__` exports** — `python_dead_code_roots_test.go:491-536` (`ConcatenatedAllExportsAreRoots`)
13. **Unknown decorators not marked** — `python_dead_code_roots_test.go:539-557` (`DoesNotMarkUnknownDecoratorsAsDeadCodeRoots`)
14. **Embedded shell commands** — `embedded_shell_test.go:12-64` (`TestDefaultEngineParsePathPythonEmbeddedShellCommands`), `engine_python_ast_parity_test.go:136-192` (`EmbeddedShellRichParity`)
15. **Generator semantic kind** — `engine_python_generator_test.go:11-38` (`TestDefaultEngineParsePathPythonGeneratorFunctionsEmitSemanticKind`)
16. **Dotted call metadata** — `engine_python_call_semantics_test.go:11-41` (`TestDefaultEngineParsePathPythonEmitsDottedCallMetadata`)
17. **Self receiver inference** — `engine_python_call_semantics_test.go:85-107` (`InfersSelfReceiverType`)
18. **Method context and inferred receiver type** — `engine_python_call_semantics_test.go:42-84` (`EmitsMethodContextAndInferredReceiverType`)
19. **Type annotations (splat params, return type)** — `engine_python_ast_parity_test.go:66-131` (`SplatTypedParamAnnotations`)
20. **Multiline class header** — `engine_python_ast_parity_test.go:17-59` (`MultilineClassHeaderUsesAST`)
21. **Lambda attribute assignment** — `engine_python_lambda_assignment_test.go:13-56` (`LambdaAttributeAssignmentEmitsNamedFunction`)
22. **Anonymous lambda** — `engine_python_lambda_assignment_test.go:57-96` (`AnonymousLambdaPromotesSyntheticFunction`)
23. **Annotated assignments** — `engine_python_annotation_assignment_test.go:12-62` (`EmitsAnnotatedAssignmentTypeAnnotations`)
24. **Rationale comments** — `engine_python_rationale_test.go:12-55` (`EmitsRationaleComments`)
25. **Value-flow taint findings** — `python_cfg_dataflow_test.go:30-99` (`DataflowOffIsByteIdentical`, `TaintSourceToSQLSink`)
26. **Interproc findings** — `python_cfg_dataflow_test.go:83-119` (`InterprocFindingAcrossFunctions`, `FunctionIDsIncludeRepositoryID`)
27. **Cyclomatic complexity** — `engine_cyclomatic_complexity_test.go:43-57` (2 test cases)
28. **Golden audit accuracy** — `goldenaudit/accuracy_test.go:114-168`

## Unverified / Claimed-but-Untested Constructs
List constructs claimed but not covered by any test.

1. **Notebook cell `source` as `[]any` or `[]string`** (`notebook.go:49-57`): the empty notebook is tested (`notebook_test.go`), but multi-string arrays are not explicitly tested.
2. **Dunder protocol hook with `type_alias_statement`** (`dead_code_roots.go:294-300`): tested via `type().method = method` pattern but not explicitly isolated.
3. **Empty notebook source** (`notebook.go:23-24`): tested for `no code cells`, but not for `cells: []`.
4. **Invalid notebook JSON** (`notebook.go:18-19`): error path not tested in subdirectory tests.
5. **Import resolution with `__init__.py` fallback** (`imports.go:233-235`, `pythonResolveImportCandidate`): no test with actual filesystem resolution.
6. **`pythonTrailingName` with edge case separators** (`call_inference.go:81-93`): not tested with backslashes or colons.

## Edge Cases Considered
List edge cases the tests actually cover with test references.

- **Reversed script main guard** (`"__main__" == __name__`) — `python_dead_code_roots_test.go:199-232`
- **Script guard skips else branches and nested definitions** — `python_dead_code_roots_test.go:239-288`
- **Dunder method assignment via `type(x).__reduce__ = __reduce__`** — `python_dead_code_roots_test.go:292-337`
- **Concatenated `__all__` literals** — `python_dead_code_roots_test.go:491-536`
- **Multiline class header** — `engine_python_ast_parity_test.go:17-59`
- **Orphaned route decorator** (no following def) — tested via correlation-truth contract (#2788), not emitting fabricated handler
- **Splat typed parameters** — `engine_python_ast_parity_test.go:66-131`
- **Embedded shell alias shadowing** — `engine_python_ast_parity_test.go:136-192`
- **Module-level call skip for embedded shell** — `engine_python_ast_parity_test.go:136-192`
- **Lambda assignment promotes synthetic function** — `engine_python_lambda_assignment_test.go:13-56`
- **Value-flow gate off is byte-identical** — `python_cfg_dataflow_test.go:30-64`
- **Duplicate method reference in `__all__` and `package_init_export`** — deduplication tested in public API root tests
- **Generator yield in nested function is inner-only** — `engine_python_generator_test.go:39-63`
- **Empty constructor caller (`no args`)** — not explicitly tested (minor)

## Edge Cases NOT Considered
List edge cases not tested.

- **Python file with BOM (byte order mark)**
- **Notebook with `cell_type: "markdown"` and `cell_type: "raw"`**
- **Python 2-style `print` statement** (not valid in current grammar)
- **`@staticmethod` and `@classmethod` decorators as roots**
- **Circular relative import (`from ... import`)**
- **Class body `assignment` versus `annotated_assignment`** for `__tablename__` in SQLAlchemy
- **Function inside function inside function** (nested beyond 2 levels)
- **`def __init_subclass__` outside a class body**
- **`@typer.cached_property` variant** (property detection checks suffix match only)

## Verdict
deep

The Python parser has 27 parent-level engine tests, 4 subdirectory test files, dedicated tests for every dead-code root kind (including `dataclass_post_init` via `engine_python_dead_code_semantics_test.go`), framework semantics, ORM mappings, embedded shell, generators, lambda assignments, annotated assignments, type annotations, call inference, rationale comments, value-flow, class-reference call items, and notebook extraction. Tests cover edge cases like reversed script guards, concatenated `__all__` exports, splat-typed parameters, and alias shadowing in embedded shell. Only a few tertiary code paths (notebook `[]any` source, explicit invalid-JSON error) lack dedicated tests.

## Recommended Actions
1. Add an explicit test for notebook with `source` as `[]any` array.
2. Add a test for `staticmethod` and `classmethod` decorators not being marked as dead-code roots.
