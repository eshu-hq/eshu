# Dead Code Language Maturity

This page records the current language-specific maturity for
`code_quality.dead_code`. It mirrors the query package's maturity map and keeps
the long language inventory out of the main reachability spec.

For the analyzer contract, response fields, and promotion rules, see
[Dead Code Reachability Spec](dead-code-reachability-spec.md).

## Maturity States

| State | Meaning |
| --- | --- |
| `derived` | Eshu can return graph-backed candidates with some modeled roots, but exact cleanup-safe truth is not proven. |
| `derived_candidate_only` | Eshu can index the language and return candidates, but language roots are too thin for cleanup-ready answers. |
| `non_code_iac_evidence` | Eshu indexes infrastructure/configuration evidence, but `code_quality.dead_code` does not return source-code cleanup candidates for the language. |
| `unsupported_language` | The language is outside this capability's parser/indexing contract. |
| reserved `exact` | Fixture, root, reachability, backend, API, MCP, CLI, and performance gates prove cleanup-safe answers for the scope. No language is documented at this level here. |
| reserved `ambiguous_only` | Eshu can identify uncertainty but should not return actionable unused symbols for the scope. No language is documented at this level here. |

## Current Matrix

| Language | Current maturity | Modeled roots or evidence | Main exactness blockers |
| --- | --- | --- | --- |
| C | `derived` | `main`, directly included local header declarations, signal handlers, callback arguments, direct function-pointer initializer targets. | Macro expansion, conditional compilation, build-target selection, transitive include graphs, broad callback registration, dynamic symbol lookup, external linkage. |
| C# | `derived` | `Main`, constructors, overrides, same-file interface methods and implementations, derived ASP.NET controller-action roots, hosted-service callbacks, test methods, serialization callbacks. | Reflection, dependency injection, source generators, partial types, dynamic dispatch, project references, broad public API surfaces. |
| C++ | `derived` | `main`, directly included local header declarations, virtual and override methods, callback arguments, direct function-pointer initializer targets, Node native-addon entrypoints. | Macro expansion, conditional compilation, build targets, transitive includes, templates, overload resolution, broad virtual dispatch, callback registries, dynamic symbol lookup, external linkage. |
| Dart | `derived` | Top-level `main`, constructors, named constructors, `@override` methods, Flutter `build` and `createState`, public `lib/` declarations outside `lib/src/`. | Part libraries, conditional imports/exports, package exports, dynamic dispatch, Flutter route and lifecycle wiring, generated code, mirrors, broad public API surfaces. |
| Elixir | `derived` | Application `start/2`, public macros and guards, `@impl` behaviour callbacks, GenServer and Supervisor callbacks, Mix task `run/1`, protocols, Phoenix controller actions, LiveView callbacks. | Macro expansion, dynamic dispatch, behaviour callback resolution, protocol dispatch, Phoenix routes, supervision trees, Mix environment selection, broad public API surfaces. |
| Go | `derived` | `main`, `init`, Cobra registrations and signatures, stdlib HTTP registrations and signatures, controller-runtime `Reconcile`, exported non-`cmd`/`internal`/`vendor` package symbols, direct method calls, function values, generic constraints, interface implementations, type references, dependency-injection callbacks. | Broader routers, webhook and worker registrations, reflection, build tags, build-target selection, broader package-public API and plugin behavior. |
| Groovy | `derived_candidate_only` | Jenkinsfile pipeline entrypoints and Jenkins shared-library `vars/*.groovy` `call` methods. | Dynamic dispatch, closure delegates, Jenkins shared-library loading, pipeline DSL dynamic steps. |
| HCL | `non_code_iac_evidence` | Terraform and Terragrunt entities appear on infrastructure, repository-context, language-query, and relationship-evidence surfaces. | Terraform plan/state liveness, module/reference graph resolution, workspace and variable selection, dynamic block expansion, Terragrunt runtime includes. |
| Haskell | `derived` | `main`, module exports, exported types, typeclass methods, instance methods. | Template Haskell, CPP conditionals, Cabal component selection, implicit exports, typeclass dispatch, module reexports, FFI. |
| Java | `derived` | Main methods, constructors, overrides, Ant setters, Gradle plugin/task/DSL roots, method references, Spring roots, lifecycle callbacks, JUnit roots, Jenkins/Stapler roots, serialization hooks, literal reflection, ServiceLoader providers, Spring autoconfiguration metadata. | Broad dynamic dispatch, dependency injection, annotation processing, string-built reflection, generated code, source-set and public API surfaces. |
| JavaScript | `derived` | Next.js routes and app exports, Express/Koa/Fastify/NestJS/Hapi roots, CommonJS and package exports, package `bin`, seed and migration exports, AMQP consumers, proxy callbacks. | Dynamic imports, property dispatch, runtime plugin loading, framework discovery, package export breadth, declaration/API precision. |
| Kotlin | `derived` | Top-level `main`, constructors, interfaces, interface implementations, overrides, Gradle roots, Spring roots, lifecycle callbacks, JUnit roots. | Reflection, dependency injection, annotation processing, compiler plugins, dynamic dispatch, Gradle source sets, multiplatform targets, broad public API surfaces. |
| Perl | `derived` | Script entrypoints, package namespaces, Exporter `@EXPORT` and `@EXPORT_OK`, constructors, special blocks, `AUTOLOAD`, `DESTROY`. | Symbolic references, AUTOLOAD target resolution, `@ISA`, Moose/Moo metadata, import side effects, runtime `eval`, broad public API surfaces. |
| PHP | `derived` | Script entrypoints, constructors, magic methods, interface methods and implementations, trait methods, controller actions, literal route handlers, Symfony route attributes, WordPress hook callbacks. | Dynamic dispatch, reflection, Composer/autoload surfaces, include/require resolution, broader framework routes, trait resolution, namespace aliases, broad public API surfaces. |
| Python | `derived` | `__main__`, script guards, FastAPI, Flask, Celery, Click, Typer, AWS Lambda handlers, dataclass roots, properties, dunder protocol methods, `__all__`, package `__init__.py` reexports, public base classes and members proven by parser evidence. | Dynamic imports, reflection-like dispatch, worker discovery beyond modeled decorators, framework plugins, non-export-declared public APIs. |
| Ruby | `derived` | Rails controller actions, Rails callback methods, literal method-reference targets, `method_missing` and `respond_to_missing?`, script guards. | Broader metaprogramming, autoload and constant resolution, framework route files, gem public API surfaces. |
| Rust | `derived` | Cargo entrypoints, tests, Tokio runtime/test functions, exact `pub` API items, Criterion benchmarks, direct trait implementation methods, Cargo auxiliary-target exclusions, conditional derive evidence, direct file module-resolution status, literal macro-body module/import declarations. | Arbitrary macro expansion, `cfg` and Cargo feature solving, cross-crate semantic module resolution, broad trait dispatch. |
| Scala | `derived` | `main`, objects extending `App`, traits and trait methods, same-file trait implementations, overrides, Play controller actions, Akka actor `receive`, lifecycle callbacks, JUnit methods, ScalaTest suites. | Macros, implicit and given/using resolution, dynamic dispatch, reflection, sbt source sets, framework route files, compiler plugins, broad public API surfaces. |
| SQL | `derived` | Stored routines are candidates; parser-proven trigger-to-function `EXECUTES` edges protect trigger-invoked routines. | Dynamic SQL, dialect-specific routine resolution, migration order. |
| Swift | `derived` | `main`, `@main` types, SwiftUI app types and `body`, protocol types and methods, protocol implementations, constructors, overrides, UIKit application delegate callbacks, Vapor route handlers, XCTest, Swift Testing. | Macros, conditional compilation, SwiftPM target resolution, protocol witnesses, dynamic dispatch, property wrappers, result builders, Objective-C runtime dispatch, broad public API surfaces. |
| TSX | `derived` | React and Next.js roots through the JavaScript-family parser, component exports, generated/test exclusions. | React runtime dispatch, dynamic imports, JSX component indirection, package declaration surfaces, framework loading. |
| TypeScript | `derived` | JavaScript-family framework roots plus interface method implementations, module-contract exports, static registry members, public API exports, public API reexports, public API type references. | Dynamic imports, property dispatch, declaration-surface precision, package export breadth, decorators, framework/plugin loading. |

## Evidence Locations

The source of truth is split by owner:

| Evidence | Location |
| --- | --- |
| Language maturity map | `go/internal/query/code_dead_code_language_maturity.go` |
| Response contract | `go/internal/query/code_dead_code_analysis.go` |
| Candidate scan and incoming-edge policy | `go/internal/query/code_dead_code.go`, `go/internal/query/code_dead_code_scan.go` |
| Fixture contract | `tests/fixtures/deadcode/README.md` |
| Parser support summary | [Parser Support Matrix](../languages/support-maturity.md) |

Keep per-language fixture inventories in the fixture README and parser package
tests. This page should stay at the maturity and exactness-boundary level.

## Promotion Rule

Maturity is language scoped. Promoting one language to a stronger state does not
promote the full `code_quality.dead_code` capability.

Each promotion must update the query maturity map, the relevant parser and query
tests, the language page, this matrix, and the capability evidence in the same
pull request.

## Java Imported Receiver Evidence

No-Regression Evidence: issue #3004 keeps Java imported receiver call edges
bounded to parser-proven imports and one import-bound class file. Focused
regressions cover a positive imported receiver edge, duplicate import-bound
source roots before method lookup, and fully qualified receiver declarations
that conflict with a same-leaf import. The local proof was
`go test ./internal/parser/java -run TestParseEmitsQualifiedJavaReceiverType -count=1`
and `go test ./internal/reducer -run 'TestResolveGenericCallee(LeavesDuplicateJavaImportBindingUnresolvedBeforeMethodLookup|DoesNotBindQualifiedJavaReceiverToConflictingImport|UsesJavaImportedReceiverBeforeAmbiguousRepoName|LeavesAmbiguousJavaImportedReceiverUnresolved)' -count=1`.

No-Observability-Change: the resolver uses existing parsed import rows,
repository prescan import maps, parser receiver metadata, and the in-memory
code entity index before code-call row emission. It adds no graph query, graph
write shape, queue table, worker, lease, batch setting, runtime knob, metric
instrument, metric label, span, route, or log key; operators still diagnose
code-call extraction through existing reducer execution spans/counters and the
`code call materialization completed` log fields.

## TypeScript Direct Import Evidence

No-Regression Evidence: issue #3004 keeps unqualified TypeScript calls bounded
to parser-proven direct imports before weak repository-wide fallback. The local
proof was
`go test ./internal/resolutionparity -run TestGoldenCallGraphCorrectnessHarness/typescript_import_binding -count=1`,
which failed before the resolver emitted `repo_unique_name` for
`import { helper } from "./lib"; helper()`, then passed after the
JavaScript-family import-binding branch ran before repository fallback.
`go test ./internal/reducer -run TestExtractCodeCallRowsBlocksTypeScriptDirectImportFallbackToRepoUnique -count=1`
failed before unresolved parser-proven direct imports could block an unrelated
repo-unique helper, then passed after unresolved direct imports stayed
unresolved instead of fabricating a weak fallback edge.
`go test ./internal/reducer -run 'TypeScript|Import|ReExport' -count=1`
proves existing TypeScript interface, baseUrl, namespace import, and static
re-export behavior still holds.

No-Observability-Change: TypeScript direct import resolution reorders an
existing in-memory resolver branch over parsed import rows, repository prescan
import maps, and the existing static reexport index. It adds no graph query,
graph write shape, queue table, worker, lease, batch setting, runtime knob,
metric instrument, metric label, span, route, or log key; operators still
diagnose code-call extraction through existing reducer execution
spans/counters and the `code call materialization completed` log fields.
