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
| Runtime validation | Compose-backed fixture verification; see [Local Testing](../reference/local-testing.md), plus the offline real-repo dogfood check `scripts/dogfood-java.sh` (see [Support Maturity](#support-maturity)) |

## Supported Surfaces

| Surface | Current contract |
| --- | --- |
| Source entities | Methods, constructors, classes, interfaces, enums, annotation types, local variables, fields, imports, method invocations, and object creation. |
| Call metadata | Method and constructor arity, local receiver type inference, argument counts, typed receiver variables, records, nested-class context, and same-class helper return types. |
| Annotation metadata | Applied annotations persist as first-class graph entities and remain graph-first on `code/language-query`, with content fallback when the graph is empty. |
| Dead-code roots | Parser metadata and reducer `REFERENCES` edges suppress parser-proven runtime and framework roots from cleanup candidates. |
| Framework route entries | Literal Spring MVC/WebFlux, JAX-RS, and Micronaut route annotations emit exact `framework_semantics.<framework>.route_entries` with handler names. The shared reducer resolves each entry to a `HANDLES_ROUTE` edge, proven through the reducer's real `HANDLES_ROUTE` intent resolution and the `trace_route_callers` surface reading graph rows derived from that intent (a fake graph reader, not a materialized graph). |

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
| Real-repo validation | `real-repo-validated` (#5399) |
| End-to-end indexing | `fixture-backed` |
| Dead-code exactness | `derived`, not cleanup-safe exact truth |

Real-Repo Validation earned `real-repo-validated` (#5399) through a committed,
offline-reproducible dogfood artifact: `scripts/dogfood-java.sh` runs the
standing `TestDogfoodJavaRealRepoSnapshot` regression test
(`go/internal/parser/java/dogfood_real_repo_test.go`) against the committed
app-shaped corpus at `tests/fixtures/dogfood/java_real_repo` (a synthetic
Spring Boot-style `src/main/java` + `src/test/java`
controller/service/model layout; no external repository or pinned SHA is
cited as provenance here, since this page never carried a specific
external-repo dogfood claim to preserve) and diffs the parser's bucket counts
against the checked-in snapshot at
`go/internal/parser/java/testdata/dogfood_real_repo_snapshot.txt`. The script
requires no network access or Docker. End-to-end indexing stays
`fixture-backed`: the corpus is not staged in `corpus_fixtures` in
`scripts/verify-golden-corpus-gate.sh` and has no B-12 attribution, so it does
not clear the `supported` bar (see
[Parser Support Matrix](support-maturity.md#grade-definitions)).

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
  positive, unknown-handler, and ambiguous-handler fixtures. Proven at parser,
  reducer, and query tiers: `TestHandleRouteToCallerResolvesJavaSpringHandler`
  parses a real Spring MVC fixture, resolves the handler through the reducer's
  real `HANDLES_ROUTE` intent resolution, and asserts `trace_route_callers`
  returns that handler from graph rows derived from the reducer's intent (a
  fake graph reader, not a materialized graph). The stages the case does not
  execute — intent-to-edge projection and the live-graph read — contain no
  per-framework code paths and are proven generically by
  `go/internal/reducer/handles_route_projection_process_test.go` (intent to
  `HANDLES_ROUTE` edge write with endpoint-presence gating),
  `go/internal/storage/cypher/edge_writer_handles_route_test.go` (edge Cypher
  dispatch), and `go/internal/query/code_route_to_caller_live_test.go` (live
  NornicDB read of a materialized `HANDLES_ROUTE` edge).
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
