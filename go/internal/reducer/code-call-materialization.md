# Reducer Code-Call Materialization

This guide holds the detailed `code_call_materialization` contract. Keep the
package overview in `README.md`; keep resolver ordering, parser metadata rules,
and performance gotchas here.

## Resolver contract

`ExtractCodeCallRows` turns parser `function_calls` and SCIP call facts into
canonical `CALLS` edges. Native parser calls resolve in this order: same-file
symbols, Go package-qualified import targets, Go method-return chains, Go
same-directory symbols, repository-unique symbols, then imported cross-file
symbols when the prescan import map proves the target file. For
JavaScript-family files, import resolution also honors parser-proven namespace
aliases, CommonJS property requires such as `require("./x").handler`, CommonJS
`module.exports` self-aliases, tsconfig `baseUrl` `resolved_source` metadata,
one bounded hop through static relative re-export barrels, and dynamic
JavaScript imports whose runtime `.js` specifiers point at TypeScript source.
Qualified JavaScript-family calls that match an imported namespace resolve the
imported target before trying same-file trailing-name matches, so controller and
model functions with the same method name do not collapse into self-calls.
Unqualified JavaScript-family calls still let same-file symbols win first, but
parser-proven direct imports resolve before weak repository-wide fallback so
`import { helper } from "./lib"; helper()` records `import_binding` instead of
promoting a unique bare name to repo-wide truth.
Constructor calls, local receiver type metadata, returned and
constructor-argument function-value references, TypeScript type references, and
Function prototype receiver calls such as `callback.call(...)` let `new Type()`,
`value.method()`, type-only imports, worker processors, route-handler callback
objects, callback returns, and function receiver dispatch resolve when parser
evidence proves the local target. Static object registries are resolved only
inside the containing function source, including destructured aliases and
literal bracket keys; runtime-computed keys do not create edges. `JavaScript`
static alias metadata is cached on the code entity index
(`code_call_materialization_index.go:45`) and reused during dynamic call
resolution (`code_call_materialization_dynamic_javascript.go:41`), so
generated bundles with thousands of call sites do not re-parse the same
containing function source for every call. Sources with no static aliases are
cached too; a negative scan is still the proof that the reducer can skip the
expensive regex pass on later calls in the same source span.

TypeScript interface-typed receiver calls resolve only when parser evidence
proves exactly one local class implements the interface and declares the called
method. Multiple implementers, external interfaces, generic or union receiver
types, and missing `implemented_interfaces` evidence stay unresolved.

For package entrypoint, package bin, package export, and top-level JavaScript
reference files, the repository scoped `File.uid` may be the caller so
executable module bodies can make `main()`, constructor, member,
function-value, and type-reference edges reachable without treating every
library module as a root. Code-call rows now carry `caller_entity_type` and
`callee_entity_type` from the entity index, using `File` for repository-scoped
file-root callers, so the graph writer can use the exact endpoint label and
`uid` instead of a broad label family. The Go same-directory step applies to
functions and type entities from `structs` and `interfaces`; command packages
commonly reuse local helper names such as `wireAPI` in sibling `cmd/*`
directories, so repo-wide bare-name resolution must stay ambiguous in that
case. Go package-qualified resolution maps parser import metadata such as
`github.com/hashicorp/terraform/internal/actions` to matching repository
directories before resolving calls like `actions.NewActions()`, and honors
explicit Go import aliases through parser `alias` metadata. Go method-return
chain resolution uses parser-provided `return_type`, `chain_receiver_obj_type`,
and `chain_receiver_method` metadata, so `ctx.Actions().GetActionInstance()`
can reach `Actions.GetActionInstance` only after the parser proves that `ctx`
has a receiver type whose `Actions` method returns `Actions`.

For Java, parser-provided `inferred_obj_type` metadata lets
receiver-qualified calls such as `factory.basicAuth(...)` resolve to methods
on the parsed receiver type when local syntax proves the variable, parameter,
field, or inline constructor receiver. Enhanced-for variables use the same
receiver metadata, so loop-local calls such as `alignment.accepts(...)`
resolve to the record or class declared in the loop header. Unqualified calls
inside nested classes use parser-proven `enclosing_class_contexts` as exact
candidates, so an inner helper wins before the reducer tries the enclosing
class method. Explicit outer-this field receivers in Java's
named-outer-instance field form use the enclosing class field type to resolve
calls on collaborator objects. `code_call_materialization_arity.go` converts
`argument_count` and `parameter_count` metadata into `name#arity` candidates
before broad name matching, so overloaded methods such as `basicAuth(String)`
and `basicAuth(String, String)` do not collapse into one reachability result.
When Java parser rows also carry `argument_types` and `parameter_types`, the
reducer adds type-signature candidates such as
`configureBootJarTask(BootJar,TaskProvider)` before falling back to broader
names. That lets class-literal typed Gradle lambdas and helper-call return
values resolve overloaded callback methods without treating every same-name
overload as reached. When the receiver type is imported, the Java resolver
checks the parser import row and the repository prescan import map before
repo-wide fallback, then resolves the method only inside the imported class
file. Ambiguous same-name classes in other packages stay unresolved unless an
explicit import binds the receiver to exactly one file. Duplicate source roots
for the same imported class stay unresolved instead of picking the first path;
fully qualified receiver declarations must agree with the import source before
they can use this shortcut, so `com.other.Service service` is not rebound to
`import com.acme.Service`.
successful imported receiver matches are recorded as `import_binding`.

Parser rows with `call_kind=java.method_reference` resolve method-reference
syntax such as `this::configureTask` to same-class methods and materialize as
`REFERENCES`, because the source proves reachability through a functional
callback without proving an immediate invocation. This keeps Java method
reachability bounded to evidence from the parsed files instead of treating
every method with the same name as live. Parser rows with
`call_kind=java.reflection_class_reference` and
`call_kind=java.reflection_method_reference` also materialize as `REFERENCES`
when the parser saw literal class or method names in reflection calls. Dynamic
reflection strings stay unmodeled. Java metadata files produce
`call_kind=java.service_loader_provider` and
`call_kind=java.spring_autoconfiguration_class` rows; the reducer uses the
metadata file as the caller and the referenced provider or auto-configuration
class as the callee.

For Python, parser-provided `class_context`, `inferred_obj_type`, and
`constructor_call` metadata keep method and constructor resolution bounded to
evidence in the parsed file. Local constructor assignments and `self` member
calls can both carry receiver type. Constructor calls can reach both the class
entity and its `__init__` method. Class receiver rows with
`call_kind=python.class_reference` materialize as `REFERENCES`, while a
class-qualified method call may resolve to a unique inherited method name when
no exact class-context method exists. That protects dataclass and model helper
paths without making all same-named Python methods reachable.

Parser metadata rows with `call_kind=go.composite_literal_type_reference`,
`call_kind=typescript.type_reference`, `call_kind=python.class_reference`, or
`call_kind=java.method_reference`, plus Java literal-reflection, ServiceLoader,
and Spring auto-configuration class references, materialize as deduplicated
`REFERENCES` edges. They prove reference roots for dead-code classification,
but must not materialize as `CALLS` because that would make graph truth claim
that type or class references are invocations.

`CodeCallMaterializationHandler` logs `code call materialization completed`
with fact count, stable symbol key count, active symbol-definition fact count,
repository count, row counts, and timing for scoped fact load, context build,
active symbol-definition load, extraction, intent build, intent upsert, and
total duration. Keep that signal when changing the handler; it is the first
split used to tell parser extraction cost from Postgres lookup and intent-write
cost on large repositories.

SCIP edges bypass the heuristic resolver when both caller and callee locations
map to known entities. When a SCIP edge references a callee outside the caller
repository, the handler collects source-emitted stable keys such as
`callee_symbol`, loads active definition file facts that carry those exact keys,
and matches them against definition-level stable symbol keys such as
`scip_symbol`. Native parser rows can use the same active stable-symbol index
for explicit package-export symbols (`package_export_symbol`, `export_symbol`,
or `package:<package_id>#<export_name>`), which resolves cross-repo exported API
uses as `import_binding` rather than a weak repo-wide name guess. Active
definition facts are extraction-only: repository context, refresh scopes, and
delta file scopes still come from the triggering scope's facts so external
definition repositories do not receive refresh intents. Stable symbol keys are
derived from parser payload fields only; they must not include `FactID`,
generation id, or other generation-bearing storage identity. Keep the native
and SCIP paths idempotent: duplicate facts for the same caller, callee, and
reference line must collapse to one intent row before graph writes.

## Gotchas

- Bare names are scoped before they are broadened; same-file wins first, then
  language-specific bounded proofs decide whether repository-wide matching is
  safe.
- Type, class, reflection, ServiceLoader, and method-reference rows create
  reachability roots as `REFERENCES`, not invocation truth as `CALLS`.
- Generated JavaScript bundles rely on static-alias and negative-scan caching;
  do not move that work back into a per-call loop.
- The handler completion log is the first operator signal for separating parser
  extraction cost from Postgres intent-write cost.

## Resolution provenance (issue #2223)

Each materialized code-call, reference, and Python metaclass row carries a
`resolution_method` from the closed [ADR #2222](../../../docs/internal/design/2222-resolution-provenance-code-edges.md)
vocabulary (`go/internal/codeprovenance`). `resolveGenericCallee` returns the
branch that produced the match (`scip`, `same_file`, `import_binding`,
`type_inferred`, `scope_unique_name`, `repo_unique_name`); SCIP rows carry
`scip`, metaclass rows carry `declared`, and the secondary constructor edge
carries `type_inferred`. The method is descriptive, never admissive: it does not
change which edges resolve or which rows are emitted. Graph persistence of the
tiered confidence derived from this method is owned by #2224. The no-regression
and observability evidence for this change is recorded in `README.md`.

## Cross-repo symbol resolution (issue #2717)

No-Regression Evidence: `go test ./internal/reducer -run 'TestCodeCallMaterializationHandlerLoadsActiveCrossRepoSymbolDefinitions|TestExtractCodeCallRowsResolvesCrossRepo|TestCodeCallDefinitionSymbolKeysIgnoreGenerationFields' -count=1` failed before the production handler loaded active cross-scope definition facts, then passed after definition rows supplied stable symbol keys and calls matched those keys before repo-unique fallback. `go test ./internal/storage/postgres -run TestFactStoreLoadActiveCodeCallSymbolDefinitionFacts -count=1` proves the Postgres loader is active-generation, non-tombstone, file-kind, symbol-allowlist, and keyset-page bounded. Ambiguous symbol keys with more than one target are deliberately not indexed, preserving the existing unique-or-unresolved rule.

Observability Evidence: the existing `code call materialization completed` log now includes stable symbol key count, active definition fact count, and active symbol-definition load duration beside the existing fact count, repository count, row counts, and load/extract/intent/upsert timings. The change adds no metric instrument, metric label, span, route, runtime knob, queue table, or graph backend branch.

## Java imported receiver resolution (issue #3004)

No-Regression Evidence: `go test ./internal/reducer -run 'TestResolveGenericCalleeUsesJava(ImportedReceiverBeforeAmbiguousRepoName|ReceiverTypeBeforeRepoUniqueName)|TestResolveGenericCalleeLeavesAmbiguousJavaImportedReceiverUnresolved|TestExtractCodeCallRowsResolvesJava' -count=1` failed before Java imported receiver calls could beat ambiguous repository-wide same-name candidates and before duplicate import-bound class files stayed unresolved, then passed after the Java resolver used parser import rows plus the existing prescan `imports_map` to bind `inferred_obj_type` to one imported class file. `go test ./internal/reducer -run 'TestResolveGenericCallee(LeavesDuplicateJavaImportBindingUnresolvedBeforeMethodLookup|DoesNotBindQualifiedJavaReceiverToConflictingImport)' -count=1` proves duplicate import-bound class files block weak fallback before method lookup and qualified receiver declarations do not bind to conflicting same-leaf imports. `go test ./internal/parser/java -run TestParseEmitsQualifiedJavaReceiverType -count=1` proves the parser preserves qualified receiver evidence for that reducer guard. `go test ./internal/resolutionparity -run 'TestGoldenCallGraphCorrectnessHarness/java_import_binding|TestResolutionTierGoldens' -count=1` proves the source-derived Java fixture emits the exact imported target as `import_binding` and the existing tier distribution remains stable.

No-Observability-Change: Java imported receiver resolution is an in-memory resolver branch over the existing per-call parsed import rows, prescan import map, and code entity index. It adds no graph query, graph write shape, queue table, worker, lease, batch setting, runtime knob, metric instrument, metric label, span, route, or log key. Operators still diagnose code-call extraction through the existing `code call materialization completed` log fields and reducer execution spans/counters.

## TypeScript direct import resolution (issue #3004)

No-Regression Evidence: `go test ./internal/resolutionparity -run TestGoldenCallGraphCorrectnessHarness/typescript_import_binding -count=1` failed before direct unqualified TypeScript imports were considered before repo-wide fallback, reporting `repo_unique_name` instead of `import_binding`, then passed after JavaScript-family unqualified imports used parser import rows before weak repo fallback. `go test ./internal/reducer -run TestExtractCodeCallRowsBlocksTypeScriptDirectImportFallbackToRepoUnique -count=1` failed before unresolved direct imports could block an unrelated repo-unique helper, then passed after parser-proven import bindings returned unresolved instead of falling through to weak repo fallback. `go test ./internal/reducer -run 'TypeScript|Import|ReExport' -count=1`, `go test ./internal/reducer -count=1`, and `go test ./internal/resolutionparity -count=1` prove existing TypeScript interface, baseUrl, namespace import, and static re-export behavior still holds.

No-Observability-Change: TypeScript direct import resolution reorders an existing in-memory resolver branch over parsed import rows, repository prescan import maps, and the existing static reexport index. It adds no graph query, graph write shape, queue table, worker, lease, batch setting, runtime knob, metric instrument, metric label, span, route, or log key. Operators still diagnose code-call extraction through the existing `code call materialization completed` log fields and reducer execution spans/counters.
