# Java Parser

This page describes the current Go parser and query contract for Java. Detailed
parser mechanics live in `go/internal/parser/java/README.md`.

## Parser Contract

| Field | Value |
| --- | --- |
| Language | `java` |
| Family | `language` |
| Parser | `DefaultEngine (java)` |
| Entrypoint | `go/internal/parser/java_language.go` |
| Package detail | `go/internal/parser/java/README.md` |
| Fixture repo | `tests/fixtures/ecosystems/java_comprehensive/` |
| Main parser tests | `go/internal/parser/engine_managed_oo_test.go`, `go/internal/parser/java_*_test.go` |
| Runtime validation | Compose-backed fixture verification; see [Local Testing](../reference/local-testing.md) |

## Supported Surfaces

| Surface | Current contract |
| --- | --- |
| Source entities | Methods, constructors, classes, interfaces, enums, annotation types, local variables, fields, imports, method invocations, and object creation. |
| Call metadata | Method and constructor arity, local receiver type inference, argument counts, typed receiver variables, records, nested-class context, and same-class helper return types. |
| Annotation metadata | Applied annotations persist as first-class graph entities and remain graph-first on `code/language-query`, with content fallback when the graph is empty. |
| Dead-code roots | Parser metadata and reducer `REFERENCES` edges suppress parser-proven runtime and framework roots from cleanup candidates. |
| Framework route entries | Literal Spring MVC/WebFlux, JAX-RS, and Micronaut route annotations emit exact `framework_semantics.<framework>.route_entries` with handler names. The shared reducer resolves each entry to a `HANDLES_ROUTE` edge, queryable end to end through the `trace_route_callers` surface. |

Primary proof:

- `go/internal/parser/engine_managed_oo_test.go::TestDefaultEngineParsePathJava`
- `go/internal/parser/java_dead_code_roots_test.go`
- `go/internal/parser/java_dead_code_framework_roots_test.go`
- `go/internal/parser/java_kotlin_spring_route_semantics_test.go`
- `go/internal/parser/java_comprehensive_route_fixture_test.go::TestDefaultEngineParsePathJavaComprehensiveRouteFixtures`
- `go/internal/parser/java_reflection_test.go`
- `go/internal/reducer/code_call_materialization_java_reflection_test.go`
- `go/internal/reducer/handles_route_java_test.go`
- `go/internal/query/code_dead_code_java_roots_test.go`
- `go/internal/query/code_route_to_caller_java_test.go::TestHandleRouteToCallerResolvesJavaSpringHandler`

## Dead-Code Support

Java dead-code support is `derived`. Eshu suppresses parser-proven runtime
roots and uses reducer-produced `REFERENCES` edges when source or metadata
proves reachability. It does not claim whole-program exactness for broad
reflection or dependency injection.

Modeled roots and evidence include:

- JVM entrypoints: `main`, constructors, `@Override`, serialization hooks, and
  Externalizable hook signatures.
- Build and framework callbacks: Ant setters, Gradle plugin/task/DSL roots,
  Spring roots, JUnit roots, Jenkins/Stapler roots, and lifecycle callbacks.
- Java reference evidence: same-class method references, bounded literal
  reflection, ServiceLoader providers, Spring Boot AutoConfiguration imports,
  and legacy `spring.factories`.
- Overload-sensitive calls: parameter counts, argument counts, parameter types,
  class-literal argument types, typed lambda receivers, and same-class helper
  return types.

## Support Maturity

| Dimension | Status |
| --- | --- |
| Grammar routing | `supported` |
| Normalization | `supported` |
| Framework packs | Spring, Gradle, JUnit, Jenkins, Stapler, ServiceLoader, serialization, bounded reflection |
| Query surfacing | `supported` |
| Real-repo validation | `supported` |
| End-to-end indexing | `supported` |
| Dead-code exactness | `derived`, not cleanup-safe exact truth |

## Framework And Library Support

Supported today:

- Spring component classes, request-mapping methods, bean methods, scheduled
  methods, event listeners, and configuration properties are modeled as roots.
- Literal Spring MVC/WebFlux route annotations emit route entries only when the
  route path is source-literal and the handler is the annotated method. Class
  `@RequestMapping` literal prefixes and path variables are preserved. The
  reducer still emits `HANDLES_ROUTE` only when that handler name resolves
  exactly; ambiguous or unknown handlers are skipped.
- Literal JAX-RS `@Path` plus HTTP method annotations and Micronaut
  `@Controller` plus HTTP method annotations emit exact route entries when the
  path is source-literal and the handler is the annotated method.
- `HANDLES_ROUTE` materialization is framework-agnostic (the same reducer path
  every language uses) and is proven for Spring, JAX-RS, and Micronaut with
  positive, unknown-handler, and ambiguous-handler fixtures. The materialized
  edge is queryable end to end through the `trace_route_callers` MCP/API
  surface, proven against a Spring handler.
- Gradle plugin/task/DSL roots, JUnit tests and lifecycle methods,
  Jenkins/Stapler extension points, serialization hooks, ServiceLoader
  providers, and Spring Boot autoconfiguration metadata are modeled roots.
- Bounded literal reflection and method-reference evidence protect parser-proven
  references.
- Vulnerability reachability for Maven/Gradle findings can use Java imports and
  calls only after resolver evidence proves the dependency's package API prefix;
  this is a reachable prioritization signal, not a not-called or safe result.

Not claimed today:

- Arbitrary string-built reflection, runtime classpath scanning, generated
  code, broad dependency injection, annotation processors, and container
  behavior outside checked metadata remain exactness blockers.
- Spring composed/meta-annotations, non-literal route paths, multi-route
  expansion policy, external route configuration, runtime-discovered routes,
  generated handlers, and other JVM web frameworks are not claimed as exact
  route truth.

## Known Limitations

- Generic type bounds and wildcards are not captured as structured data beyond
  the receiver type leaf needed for local method-call resolution.
- Anonymous inner classes are not separately tracked.
- Lambda expressions are not individually modeled as functions.
- Dead-code does not infer arbitrary string-built reflection, container-driven
  dependency injection, runtime classpath scanning, or framework behavior that
  is not represented in source or checked metadata files.

## Parser Performance

The Java parser collapses independent full-tree tree-sitter walks into shared
passes: the Spring, JAX-RS, and Micronaut route detectors run in one
annotation walk, the declared-type-name counts are gathered during the
method-reference index walk (and classified afterward, once the counts are
complete), and the dataflow emit and interproc-effects passes share a single
traversal. This lowers the default-path full-tree walk count from 7 to 4 (and
the EmitDataflow add-on from 4 to 2) while keeping parser output byte-identical,
verified by a one-time old-vs-new `0/0` symmetric-diff over the fixture corpus
via the opt-in `JAVA_PARSE_DUMP` harness (`equivalence_dump_test.go`, a manual
differential — not a standing CI gate); standing regression protection comes
from the Java parser package tests and the B-12 golden snapshot (epic #4831,
#4838).
Contributors adding a new index builder should extend the relevant shared pass
rather than add another full-tree walk when the builder has no dependency on
another builder's completed output.
