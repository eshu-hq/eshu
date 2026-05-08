# Python Parser

This page tracks the checked-in Go parser contract in the current repository state.
Canonical implementation: `go/internal/parser/registry.go` plus the entrypoint and tests listed below.

## Parser Contract
- Language: `python`
- Family: `language`
- Parser: `DefaultEngine (python)`
- Entrypoint: `go/internal/parser/python_language.go`
- Fixture repo: `tests/fixtures/ecosystems/python_comprehensive/`
- Unit test suites: `go/internal/parser/engine_python_semantics_test.go`, `go/internal/parser/engine_python_metaclass_test.go`
- Integration validation: compose-backed fixture verification (see `../reference/local-testing.md`)

## Capability Checklist
| Capability | ID | Status | Extracted Bucket/Key | Required Fields | Graph Surface | Unit Coverage | Integration Coverage | Rationale |
|-----------|----|--------|------------------------|-----------------|---------------|---------------|----------------------|-----------|
| Functions | `functions` | supported | `functions` | `name, line_number` | `node:Function` | `go/internal/parser/engine_test.go::TestDefaultEngineParsePathPython` | Compose-backed fixture verification | - |
| Classes | `classes` | supported | `classes` | `name, line_number` | `node:Class` | `go/internal/parser/engine_test.go::TestDefaultEngineParsePathPython` | Compose-backed fixture verification | - |
| Module docstrings | `module-docstrings` | supported | `modules` | `name, line_number, docstring` | `node:Module` | `go/internal/parser/engine_python_module_semantics_test.go::TestDefaultEngineParsePathPythonModuleDocstringEmitsModuleMetadata`, `go/internal/reducer/semantic_entity_materialization_test.go::TestExtractSemanticEntityRowsFiltersAnnotationTypedefTypeAliasComponentAndFunctionFacts`, `go/internal/query/entity_summary_test.go::TestBuildEntitySemanticSummaryPythonModuleDocstring`, `go/internal/query/entity_story_test.go::TestGetEntityContextUsesGraphPythonModuleDocstringWithoutContent`, `go/internal/query/python_semantics_promotion_test.go::TestPythonSemanticProfileFromMetadataDocstringSignal` | Compose-backed fixture verification | Python module docstrings now materialize as first-class `Module` entities on the Go parser/reducer/query path, and graph-backed query/context/story surfaces promote them as `documented_module` with a first-class `python_semantics` docstring signal when the graph row already carries the docstring. |
| Imports | `imports` | supported | `imports` | `name, line_number` | `relationship:IMPORTS` | `go/internal/parser/engine_test.go::TestDefaultEngineParsePathPython` | Compose-backed fixture verification | - |
| Function calls | `function-calls` | supported | `function_calls` | `name, line_number` | `relationship:CALLS` | `go/internal/parser/engine_test.go::TestDefaultEngineParsePathPython` | Compose-backed fixture verification | - |
| Variables | `variables` | supported | `variables` | `name, line_number` | `node:Variable` | `go/internal/parser/engine_test.go::TestDefaultEngineParsePathPython` | Compose-backed fixture verification | - |
| Class and member call metadata | `class-member-call-metadata` | supported | `functions`, `function_calls` | `class_context`, `inferred_obj_type`, `call_kind` | `relationship:CALLS`, `relationship:REFERENCES` | `go/internal/parser/engine_python_call_semantics_test.go::TestDefaultEngineParsePathPythonEmitsMethodContextAndInferredReceiverType`, `go/internal/parser/engine_python_dead_code_semantics_test.go::TestDefaultEngineParsePathPythonEmitsConstructorPropertyAndClassReferenceMetadata`, `go/internal/reducer/code_call_materialization_cross_file_python_test.go::TestExtractCodeCallRowsResolvesPythonClassAndInferredInstanceMethodCalls`, `go/internal/reducer/code_call_materialization_cross_file_python_test.go::TestExtractCodeCallRowsResolvesPythonConstructorsInheritedClassMethodsAndClassReferences` | Local dogfood validation | Python methods now carry their enclosing class name, member calls on variables assigned from simple local constructors carry bounded receiver-type metadata, class constructor calls emit constructor metadata, and classmethod receivers emit class-reference metadata. The reducer uses those exact names before broad same-name fallback, so class methods, constructors, and instance methods can protect live methods without whole-program inference. |
| Dead-code root hints | `dead-code-root-hints` | supported | `functions.dead_code_root_kinds`, `classes.dead_code_root_kinds` | `name, line_number, dead_code_root_kinds` | `graph:metadata + code/dead-code policy` | `go/internal/parser/python_dead_code_roots_test.go::TestDefaultEngineParsePathPythonEmitsDeadCodeRootKinds`, `go/internal/parser/python_dead_code_roots_test.go::TestDefaultEngineParsePathPythonEmitsDeadCodeCLIRootKinds`, `go/internal/parser/python_dead_code_roots_test.go::TestDefaultEngineParsePathPythonEmitsScriptMainGuardRoot`, `go/internal/parser/python_dead_code_roots_test.go::TestDefaultEngineParsePathPythonEmitsSAMHandlerDeadCodeRootKind`, `go/internal/parser/python_dead_code_roots_test.go::TestDefaultEngineParsePathPythonEmitsPublicAPIRootKinds`, `go/internal/parser/engine_python_dead_code_semantics_test.go::TestDefaultEngineParsePathPythonEmitsConstructorPropertyAndClassReferenceMetadata`, `go/internal/query/code_dead_code_python_roots_test.go::TestHandleDeadCodeExcludesPythonFrameworkRootsFromMetadata`, `go/internal/query/code_dead_code_python_roots_test.go::TestHandleDeadCodeExcludesPythonCLIRootsFromMetadata`, `go/internal/query/code_dead_code_python_roots_test.go::TestHandleDeadCodeExcludesPythonScriptMainAndAnonymousLambdaRoots`, `go/internal/query/code_dead_code_python_roots_test.go::TestHandleDeadCodeExcludesPythonLambdaHandlerRootFromMetadata`, `go/internal/query/code_dead_code_python_roots_test.go::TestHandleDeadCodeExcludesPythonDataclassAndPropertyRootsFromMetadata`, `go/internal/query/code_dead_code_python_public_roots_test.go::TestHandleDeadCodeExcludesPythonPublicAPIRootsFromMetadata`, `go/internal/query/code_dead_code_python_public_roots_test.go::TestHandleDeadCodeExcludesPythonDunderRootsFromMetadata` | Local dogfood validation | FastAPI, Flask, Celery, Click, Typer, Python script-main guards, AWS Lambda handlers, dataclass models, dataclass post-init hooks, properties, Python dunder protocol methods, same-module `__all__` exports, package `__init__.py` reexports, public base classes inherited by parser-proven public API classes, public methods on those classes, and anonymous synthetic lambdas are emitted as parser metadata or query policy and excluded from dead-code cleanup candidates by the query policy while Python remains `derived` rather than `exact`. |
| Decorators | `decorators` | supported | `functions` | `name, line_number, decorators` | `graph:code/language-query metadata + first-class python_semantics bundle + graph-first entity_context.story + entities/resolve + code/search` | `go/internal/parser/engine_python_semantics_test.go::TestDefaultEngineParsePathPythonDecoratedFunctionsEmitDecoratorMetadata`, `go/internal/query/language_query_result_test.go::TestBuildLanguageResult_AttachesPythonGraphMetadataAndSemanticSummary`, `go/internal/query/language_query_metadata_test.go::TestEnrichLanguageResultsWithContentMetadataPreservesPythonGraphMetadata`, `go/internal/query/code_search_metadata_test.go::TestEnrichGraphSearchResultsWithContentMetadata`, `go/internal/query/code_search_metadata_test.go::TestHandleSearchReturnsGraphBackedPythonDecoratedClassWithoutContent`, `go/internal/query/code_dead_code_python_semantics_test.go::TestHandleDeadCodeReturnsGraphBackedPythonSemantics`, `go/internal/query/entity_content_fallback_test.go::TestResolveEntityReturnsGraphBackedPythonDecoratedClassWithPythonSemantics`, `go/internal/query/entity_content_python_resolve_test.go::TestResolveEntityFallsBackToContentBackedPythonDecoratedFunction`, `go/internal/query/entity_content_python_resolve_test.go::TestResolveEntityFallsBackToContentBackedPythonDecoratedAsyncFunction`, `go/internal/query/entity_metadata_test.go::TestEnrichEntityResultsWithContentMetadataPrefersExistingPythonGraphMetadata`, `go/internal/query/entity_summary_test.go::TestBuildEntitySemanticSummaryPythonFunction`, `go/internal/query/entity_summary_test.go::TestBuildEntitySemanticSummaryPythonDecoratedClass`, `go/internal/query/entity_story_test.go::TestAttachSemanticSummaryAddsStoryForSemanticEntities`, `go/internal/query/entity_story_test.go::TestGetEntityContextUsesGraphPythonMetadataWithoutContent`, `go/internal/query/entity_story_test.go::TestGetEntityContextUsesGraphPythonDecoratedClassWithoutContent`, `go/internal/query/entity_story_test.go::TestGetEntityContextFallsBackToContentBackedPythonDecoratedFunction`, `go/internal/query/entity_story_test.go::TestGetEntityContextFallsBackToContentBackedPythonDecoratedAsyncFunction`, `go/internal/query/repository_story_semantics_test.go::TestBuildRepositorySemanticOverviewCountsSemanticSignals` | Compose-backed fixture verification | Decorator metadata is emitted on the Go parser path, persisted on the graph-backed semantic entity row, projected directly by `code/language-query`, and now survives both graph-backed and content-backed `entities/resolve`, entity-context/story, `code/search`, and `code/dead-code` surfaces with a dedicated `python_semantics` bundle for structured consumers. Python class rows surface as `decorated_class`, decorated functions surface as `decorated_function`, and decorated async functions surface as `decorated_async_function` on the normal Go query path. |
| Async functions | `async-functions` | supported | `functions` | `name, line_number, async` | `graph:code/language-query metadata + graph-first entity_context.story + repository_story.semantic_overview` | `go/internal/parser/engine_python_semantics_test.go::TestDefaultEngineParsePathPythonAsyncFunctionsEmitAsyncFlag`, `go/internal/query/language_query_result_test.go::TestBuildLanguageResult_AttachesPythonGraphMetadataAndSemanticSummary`, `go/internal/query/language_query_metadata_test.go::TestEnrichLanguageResultsWithContentMetadata`, `go/internal/query/code_search_metadata_test.go::TestEnrichGraphSearchResultsWithContentMetadata`, `go/internal/query/entity_content_python_resolve_test.go::TestResolveEntityFallsBackToContentBackedPythonAsyncFunction`, `go/internal/query/entity_content_python_resolve_test.go::TestResolveEntityFallsBackToContentBackedPythonDecoratedAsyncFunction`, `go/internal/query/entity_metadata_test.go::TestEnrichEntityResultsWithContentMetadataPrefersExistingPythonGraphMetadata`, `go/internal/query/entity_summary_test.go::TestBuildEntitySemanticSummaryPythonFunction`, `go/internal/query/entity_summary_test.go::TestBuildEntitySemanticSummaryPythonAsyncFunction`, `go/internal/query/entity_story_test.go::TestAttachSemanticSummaryAddsStoryForSemanticEntities`, `go/internal/query/entity_story_test.go::TestGetEntityContextUsesGraphPythonMetadataWithoutContent`, `go/internal/query/entity_story_test.go::TestGetEntityContextFallsBackToContentBackedPythonAsyncFunction`, `go/internal/query/entity_story_test.go::TestGetEntityContextFallsBackToContentBackedPythonDecoratedAsyncFunction`, `go/internal/query/repository_story_semantics_test.go::TestBuildRepositorySemanticOverviewCountsSemanticSignals` | Compose-backed fixture verification | The async flag is emitted on the Go parser path, persisted on the graph-backed semantic entity row, projected directly by `code/language-query`, and now survives both graph-backed and content-backed resolve/context/story surfaces. Async-only rows surface as `async_function`, while combined decorator+async rows preserve the stronger `decorated_async_function` contract with a first-class `python_semantics` bundle. |
| Lambda assignments | `lambda-assignments` | supported | `functions` | `name, line_number, semantic_kind=lambda` | `graph:code/language-query metadata + graph-first entity_context.story + repository_story.semantic_overview` | `go/internal/parser/engine_python_lambda_assignment_test.go::TestDefaultEngineParsePathPythonLambdaAttributeAssignmentEmitsNamedFunction`, `go/internal/parser/engine_python_lambda_assignment_test.go::TestDefaultEngineParsePathPythonAnonymousLambdaPromotesSyntheticFunction`, `go/internal/parser/engine_python_metaclass_test.go::TestDefaultEngineParsePathPythonLambdaAssignmentEmitsNamedFunction`, `go/internal/query/python_semantics_promotion_test.go::TestPythonSemanticProfileFromMetadata`, `go/internal/query/entity_summary_test.go::TestBuildEntitySemanticSummaryPythonLambdaFunction`, `go/internal/query/entity_story_test.go::TestGetEntityContextUsesGraphPythonMetadataWithoutContent`, `go/internal/query/repository_story_semantics_test.go::TestBuildRepositorySemanticOverviewCountsSemanticSignals` | Compose-backed fixture verification | Identifier-, attribute-, and anonymous inline lambdas now materialize as function entities with `semantic_kind=lambda`, are persisted on the graph-backed semantic entity row, and are promoted consistently by the normal semantic/profile/story surfaces without any Python fallback. Anonymous lambdas use a synthetic `lambda@<line>_<column>` name so they can participate in graph-first modeling without a Python runtime bridge. |
| Metaclass relationships | `metaclass-relationships` | supported | `classes` | `name, line_number, metaclass` | `graph:code/language-query metadata + graph-first entity_context.story + relationship:USES_METACLASS + code/relationships` | `go/internal/parser/engine_python_metaclass_test.go::TestDefaultEngineParsePathPythonEmitsMetaclassMetadata`, `go/internal/query/language_query_result_test.go::TestBuildLanguageResult_AttachesPythonGraphMetadataAndSemanticSummary`, `go/internal/query/language_query_metadata_test.go::TestEnrichLanguageResultsWithContentMetadataPreservesPythonGraphMetadata`, `go/internal/query/code_relationships_graph_test.go::TestHandleRelationshipsReturnsGraphBackedPythonMetaclassUsesMetaclass`, `go/internal/query/entity_summary_test.go::TestBuildEntitySemanticSummaryPythonMetaclassClass`, `go/internal/query/entity_story_test.go::TestGetEntityContextUsesGraphPythonMetadataWithoutContent`, `go/internal/query/repository_story_semantics_test.go::TestBuildRepositorySemanticOverviewCountsSemanticSignals` | Compose-backed fixture verification | Class entities now preserve `metaclass` metadata on the Go parser path, `code/language-query` projects that metadata directly from graph rows, and the normal `code/relationships` path now returns persisted `USES_METACLASS` graph edges without the old content fallback. Graph-backed entity-context/story now also preserves the graph row when metaclass metadata is already present. |
| Inheritance | `inheritance` | supported | `classes` | `name, line_number, bases` | `relationship:INHERITS` | `go/internal/parser/engine_test.go::TestDefaultEngineParsePathPython` | Compose-backed fixture verification | - |
| Type annotations | `type-annotations` | supported | `type_annotations` | `name, line_number, type` | `graph:first-class TypeAnnotation entity + graph-backed function and annotated-assignment projection + code/language/entity_context.story` | `go/internal/parser/engine_python_semantics_test.go::TestDefaultEngineParsePathPythonEmitsTypeAnnotationsBucket`, `go/internal/parser/engine_python_annotation_assignment_test.go::TestDefaultEngineParsePathPythonEmitsAnnotatedAssignmentTypeAnnotations`, `go/internal/query/language_queries_test.go::TestHandleLanguageQuery_ContentBackedEntityTypes`, `go/internal/query/language_query_result_test.go::TestBuildLanguageResult_AttachesPythonTypeAnnotationGraphMetadata`, `go/internal/query/language_query_result_test.go::TestBuildLanguageResult_AttachesPythonTypeAnnotationProjection`, `go/internal/query/entity_summary_test.go::TestBuildEntitySemanticSummaryTypeAnnotation`, `go/internal/query/entity_summary_test.go::TestBuildEntitySemanticSummaryTypeAnnotationReturn`, `go/internal/query/entity_summary_python_annotation_test.go::TestBuildEntitySemanticSummaryPythonAssignmentTypeAnnotation`, `go/internal/query/entity_summary_test.go::TestBuildEntitySemanticSummaryPythonFunctionTypeAnnotations`, `go/internal/query/entity_annotation_fallback_test.go::TestResolveEntityFallsBackToPythonAssignmentTypeAnnotationContentEntity`, `go/internal/query/entity_annotation_fallback_test.go::TestResolveEntityFallsBackToPythonParameterTypeAnnotationContentEntity`, `go/internal/query/entity_annotation_fallback_test.go::TestGetEntityContextFallsBackToPythonAssignmentTypeAnnotationContentEntity`, `go/internal/query/entity_annotation_fallback_test.go::TestGetEntityContextFallsBackToPythonParameterTypeAnnotationContentEntity`, `go/internal/query/entity_story_test.go::TestGetEntityContextUsesGraphPythonTypeAnnotationWithoutContent`, `go/internal/query/entity_story_test.go::TestGetEntityContextUsesGraphPythonTypeAnnotationsWithoutContent`, `go/internal/query/entity_story_test.go::TestAttachSemanticSummaryAddsStoryForSemanticEntities`, `go/internal/query/python_semantics_promotion_test.go::TestPythonSemanticProfileFromMetadata` | Compose-backed fixture verification | Type annotations now materialize as first-class graph-backed `TypeAnnotation` entities when the graph row exists, while both assignment-style and context-bearing parameter annotation rows are explicitly proven on the content-backed resolve/context path. Function signatures keep the compact `type_annotation_count`/`type_annotation_kinds` projection, and the normal Go query surfaces preserve both shared `semantic_profile` output and Python-specific `python_semantics` output without a Python runtime fallback. |

## Support Maturity
- Grammar routing: `supported`
- Normalization: `supported`
- Framework pack status: `supported`
- Framework packs: `fastapi-base`, `flask-base`
- Query surfacing: `supported`
- Real-repo validation: `supported`
- End-to-end indexing: `supported`
- Notes:
  - Framework evidence for FastAPI and Flask is carried by the Go parser and
    indexing path.
  - Dead-code root metadata now covers FastAPI, Flask, Celery, Click, Typer,
    Python `if __name__ == "__main__"` script-main guards, AWS Lambda handlers
    declared in bounded SAM/CloudFormation or Serverless configuration files,
    dataclass models, dataclass post-init hooks,
    properties, dunder protocol methods, same-module `__all__` exports,
    package `__init__.py` reexports, public base classes inherited by
    parser-proven public API classes, and public methods on those classes.
    These roots suppress known live entrypoints, model helpers, protocol
    callbacks, and bounded public APIs from cleanup
    candidates while Python remains a `derived` dead-code maturity language.
  - Python methods now preserve `class_context`, and member calls on variables
    assigned from simple local constructors or `self` preserve
    `inferred_obj_type`. Constructor calls also resolve to `__init__`,
    classmethod calls on class receivers can reach inherited classmethods when
    the method name is unique, and class receiver references protect model
    classes without unbounded static analysis.
  - Python function semantics for decorators, async, generator, and `semantic_kind=lambda`
    are persisted on the graph-backed semantic entity row and projected
    directly by `code/language-query`; graph-backed entity-context/story and
    `code/search` now preserve the graph row when that metadata is already
    present, including Python class rows that surface as `decorated_class`,
    and the remaining content-backed surfaces still use the shared enrichment
    path where graph data is absent.
  - Python type-annotation signal now survives as a compact
    `type_annotation_count`/`type_annotation_kinds` projection on graph-backed
    function rows and as first-class `TypeAnnotation` entities for annotated
    assignments, so the normal query surfaces do not lose annotation signal
    after materialization even though Neo4j cannot store the parser's full
    annotation maps.
  - Type annotations are queryable through the Go content-backed APIs, the
    normal entity resolve/context surfaces now fall back to content-backed
    entities, and the normal `code/search` fallback now searches
    content-backed entity names as well as source text. `code/language-query`
    now projects Python decorator, async, generator, lambda, and metaclass metadata
    directly from graph rows, while `code/search` and repository stories still
    rely on the shared enrichment path to surface those semantics as
    summaries, `semantic_profile`, and first-class `story` output when graph
    data is absent. Python metaclass ownership now persists as a graph-backed
    `USES_METACLASS` relationship on the normal `code/relationships` path,
    and graph-backed entity-context/story now preserves the graph row when
    metaclass metadata is already present.
  - Graph-backed Python metadata is now also preserved on the entity-ID
    enrichment path used by `code/dead-code` and `code/complexity`, so those
    responses keep graph-owned decorators, async flags, generator markers, and annotation signals
    when content fallback is available for the same entity.
  - Graph-backed Python docstrings now keep a Python-specific documented
    surface kind in `semantic_profile` and related story/context responses:
    classes surface as `documented_class`, functions as
    `documented_function`, and the generic `documented_entity` fallback is
    reserved for non-Python or unlabeled content.
  - Graph-backed Python query/search/context/story responses also expose a
    dedicated `python_semantics` bundle for structured consumers alongside
    `semantic_profile`; that bundle now carries the promoted `surface_kind`
    and ordered `signals` for graph-backed Python rows as well as the raw
    decorator, async, generator, lambda, metaclass, docstring, and
    type-annotation fields. Python module docstrings now surface as
    `documented_module` with a first-class `docstring` signal when the graph
    row already carries the docstring.
  - Identifier-, attribute-, and anonymous inline lambdas now materialize as Python
    function entities with `semantic_kind=lambda`, and the normal
    semantic/profile/story surfaces promote them as `lambda_function` and
    describe them as lambda functions.
  - Python generator functions now materialize with `semantic_kind=generator`,
    and the normal semantic/profile/story surfaces promote them as
    `generator_function` and describe them as generator functions.
  - Graph-backed Python decorator rows are also preserved by the normal
    `entities/resolve` surface, which now returns the same structured
    `python_semantics` bundle and `decorated_class` surface kind when the
    graph row already carries decorator metadata.
  - Python metaclass ownership is now preserved on class entities, surfaced
    directly on the graph-backed `code/language-query` path, and exposed
    through persisted `USES_METACLASS` relationships on the normal
    `code/relationships` path.


## Known Limitations
- The documented Python surface is supported end to end on the Go path.
- Remaining validation is broader corpus breadth, not a page-level parser or
  query capability gap.
