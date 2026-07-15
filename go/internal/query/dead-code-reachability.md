# Dead-Code Reachability Query Contract

This guide preserves the detailed dead-code query contract for language roots,
exactness blockers, candidate paging, and response metadata.

## Language reachability and exactness

Code dead-code queries add an analysis pass over graph rows so parser-provided
`dead_code_root_kinds`, language maturity, test/generated exclusions, and
candidate classifications are visible in the response body. Unsupported
languages such as JSON package-script metadata are suppressed from cleanup
results before classification. Requests may include a `language` filter; SQL
uses that filter to scan `SqlFunction` candidates directly, and Dart, Perl,
PHP, and Elixir
use it for language-scoped dogfood so mixed application repositories cannot fill
the page with earlier function labels before the requested language evidence is
evaluated. The analysis block also names modeled framework
roots and Go semantic roots such as same-package direct method calls, imported
receiver method calls, generic constraint methods, fmt Stringer methods,
function-value references, and function-literal reachable calls. It also
reports JavaScript package exports,
Hapi-style handler exports, Next.js
exports, Express/Koa/Fastify/NestJS callbacks, Node migration exports,
TypeScript public API exports, public API re-exports, public type-reference
roots, module-contract exports, and TypeScript interface implementation methods.
It also suppresses parser-proven Python FastAPI, Flask, Celery,
Click, Typer, AWS Lambda handler, dataclass, post-init, property, dunder
protocol, `__all__`, package `__init__.py`, public API base, and public API
member roots, Python `if __name__ == "__main__"` script-main guards, C
`c.main_function`, `c.public_header_api`, `c.signal_handler`,
`c.callback_argument_target`, and `c.function_pointer_target` roots, C++
`cpp.main_function`, `cpp.public_header_api`, `cpp.virtual_method`,
`cpp.override_method`, `cpp.callback_argument_target`, and
`cpp.function_pointer_target`, and `cpp.node_addon_entrypoint` roots, C# main,
constructor, override, interface, ASP.NET controller, hosted-service, test, and
serialization roots, Kotlin top-level main, constructor, interface, override,
Gradle, Spring, lifecycle, and JUnit roots, Swift `@main`, top-level `main`,
SwiftUI `App`/`body`, protocol, constructor, override, UIKit application
delegate, Vapor route, XCTest, and Swift Testing roots, and Java
`main`, constructor, `@Override`, Ant `Task` setter, Gradle plugin
`apply`, task action/property, task setter, task-interface method, public Gradle
DSL, same-class method-reference target roots, Spring component and callback
roots, Java lifecycle callbacks, JUnit test/lifecycle methods, Jenkins
extension and symbol roots, Jenkins initializer/data-bound setter methods, and
Stapler web methods. Java serialization hooks are suppressed from cleanup
candidates when their signatures match JVM runtime contracts, and the analysis
metadata now reports bounded Java reflection plus ServiceLoader and Spring
auto-configuration references as modeled reachability evidence. Rust roots from
parser metadata cover Cargo entrypoints, build scripts, unit tests, Tokio
runtime/test functions, exact `pub` public API items, benchmark functions
registered through parser evidence, and trait implementation methods. Rust
parser evidence also includes path-attribute modules, direct module resolution
status, literal macro-body module/import declarations, conditional derives,
nested annotations, and structured where-clause metadata.
Rust now shares the derived dead-code maturity tier with Go and Java while
exact Rust cleanup remains gated on broader semantic resolution. Rust
`benches/` and `examples/` files are treated as Cargo auxiliary targets rather
than production cleanup candidates; the same root kinds appear in
`modeled_framework_roots` so callers can explain the suppression. The analysis
payload also suppresses Ruby parser-backed Rails controller actions, Rails
callback methods, dynamic-dispatch hooks, literal method-reference targets, and
script entrypoints. Ruby exact cleanup remains gated on metaprogramming,
autoload and constant resolution, framework route files, and gem public API
surfaces. Groovy parser metadata suppresses Jenkinsfile pipeline entrypoints and
Jenkins shared-library `vars/*.groovy` `call` methods; Groovy remains
candidate-only because dynamic dispatch, closure delegate resolution, shared
library loading, and pipeline DSL steps are not resolved exactly. Haskell
parser metadata suppresses `main`, explicit module-exported functions and
types, typeclass method declarations, and instance methods. Haskell remains
non-exact because Template Haskell, CPP conditional compilation, Cabal
component membership, implicit module exports, typeclass dispatch, module
re-exports, and FFI callbacks are not resolved exactly. Elixir parser
metadata suppresses Application-backed `start/2`, public macros, public guards,
`@impl` behaviour callbacks, arity-checked GenServer and Supervisor callbacks,
Mix task `run/1`, protocol functions, protocol implementation functions,
Phoenix controller actions shaped as `action/2`, and arity-checked LiveView
callbacks. Those checks keep broad `start/2`, `main/1`, wrong-arity callbacks,
and wrong-arity controller helpers in the candidate set instead of suppressing
them as roots. Elixir remains non-exact because macro expansion, dynamic
dispatch, behaviour callback resolution, protocol dispatch, Phoenix route
resolution, supervision trees, Mix environment selection, and public API
surfaces are not resolved exactly. Dart parser metadata suppresses top-level
`main`, constructors, `@override` methods, Flutter `build` and `createState`
callbacks, and public `lib/` API declarations outside `lib/src/`; Dart remains
non-exact because part libraries, conditional imports and exports, package
export surfaces, dynamic dispatch, Flutter route/lifecycle wiring, generated
code, mirrors, and public API breadth are not resolved exactly. PHP parser
metadata suppresses script entrypoints, constructors, known magic methods,
same-file interface and trait methods, route-backed controller actions, literal
route handlers, Symfony route attributes, and WordPress hook callbacks; PHP
remains non-exact because broader autoloading, routing, reflection, and dynamic
dispatch are not resolved exactly.
Perl parser metadata suppresses script `main`, public package namespaces,
Exporter `@EXPORT` and `@EXPORT_OK` functions, package constructors, special
blocks, `AUTOLOAD`, and `DESTROY`; Perl remains non-exact because symbolic
references, AUTOLOAD target resolution, `@ISA` inheritance, Moose/Moo metadata,
import side effects, runtime `eval`, and broad public API surfaces are not
resolved exactly.
Swift parser metadata suppresses known runtime roots while exact cleanup stays
blocked on macro expansion, conditional compilation, SwiftPM target membership,
protocol witnesses, dynamic dispatch, generated property-wrapper and
result-builder code, Objective-C runtime dispatch, and broad public APIs.
Kotlin parser metadata suppresses top-level main functions, secondary
constructors, interface methods and same-file implementations, overrides,
Gradle plugin/task callbacks, Spring component and method callbacks, lifecycle
callbacks, and JUnit methods; Kotlin remains non-exact because reflection,
dependency injection, annotation processing, compiler plugins, dynamic
dispatch, Gradle source sets, Kotlin multiplatform targets, and broad public
API surfaces are not resolved exactly.
Scala parser metadata suppresses main methods, App objects, traits and trait
methods, same-file trait implementations, overrides, Play controller actions,
Akka actor receive methods, lifecycle callbacks, JUnit methods, and ScalaTest
suite classes; Scala remains non-exact because macros, implicit/given
resolution, dynamic dispatch, reflection, sbt source sets, framework route
files, compiler plugin output, and broad public API surfaces are not resolved
exactly.
The analysis payload also exposes
`dead_code_language_exactness_blockers` for language-specific non-exact areas
such as macros, reflection, dynamic dispatch, import or module resolution,
framework routing, public API surfaces, and SQL dialect/runtime behavior. Keep
the table in `code_dead_code_language_maturity.go` aligned with
`docs/public/reference/dead-code-reachability-spec.md`. HCL is reported as
`non_code_iac_evidence`, so Terraform and Terragrunt entities stay on
infrastructure, repository-context, language-query, and relationship-evidence
surfaces instead of becoming source-code cleanup candidates. SQL `SqlFunction`
routines participate in the derived candidate scan, and the query policy uses a
batched exact graph incoming probe so reducer-written `EXECUTES` edges protect
trigger-bound routines without one graph round trip per routine.
Returned candidates can also populate
`dead_code_observed_exactness_blockers` so callers can distinguish language-wide
blockers from blockers actually present in the page they received. Candidates
that carry observed exactness blockers classify as `ambiguous` rather than
cleanup-ready `unused`.
Incoming-edge reachability first consults reducer-materialized
`code_reachability_rows` for the active generation, then falls back to completed
shared-projection intent rows and the SQL graph probe where needed. The
classification is provenance-weighted: each incoming edge or path's confidence
is derived from its `resolution_method` (ADR #2222) via
`codeprovenance.Confidence`. A candidate whose strongest incoming path is at or
below the weakest tier (`repo_unique_name`, 0.50) is kept and classified
`ambiguous` with a `weak_incoming_edge:<method>` reason, rather than being
silently treated as reachable on a same-name guess. An edge with no recorded
`resolution_method` resolves to `LegacyConfidence` and is treated as strong, so
a candidate is only demoted when every incoming edge is known to be weak. A
single strong incoming edge (`scip`, `import_binding`, `same_file`, and the
other tiers above 0.50) still removes the candidate as confidently reachable.

### Concurrent, bounded, backend-safe projection

The reducer reachability projection (`reducer.CodeReachabilityProjectionRunner`)
is partitioned by the `(scope_id, generation_id, repository_id)` conflict key.
`ReplaceRepositoryRows` deletes and re-inserts exactly that triple (the row
primary key), so distinct partitions touch disjoint rows and project
concurrently without MERGE races or lost updates; inputs that share one key
(for example two source runs for the same repository generation) stay in one
ordered partition worker so their replacements never overlap. Fan-out is bounded
by the reducer worker count (`ESHU_REDUCER_WORKERS`), clamped to the host CPU
count, and is purely additive — no worker-count reduction is used as a
correctness fix.

The traversal is in-memory, uid-anchored, and single-connected-path (each entity
keeps only its strongest shortest root path), bounded by `MaxDepth` (default 10)
and `MaxVisited` (default 200000 distinct entities). When `MaxVisited` stops a
pathological mega-repo before the full reachable set is enumerated, the snapshot
is marked truncated and logged; omitted entities are **not** asserted dead
because the incoming-edge classification falls back to the completed
shared-projection intent rows and the backend graph probe for any entity absent
from the materialized slice. The projection itself performs no graph writes, so
it is backend-agnostic; backend parity is preserved by the consuming probe,
which uses the NornicDB depth-bounded hop-by-hop call-chain fallback on NornicDB
and variable-length Cypher on Neo4j-compat.

Benchmark Evidence: `BenchmarkBuildCodeReachabilityRows`
(`internal/reducer/code_reachability_projection_test.go`) projects a
50,000-entity fan-out (fan-out 4, depth 12) in ~36 ms/op, ~89 MB/op, ~325k
allocs/op on a 12-core host (`go test ./internal/reducer -run='^$'
-bench=BenchmarkBuildCodeReachabilityRows -benchmem`); the depth and visited
bounds cap a single snapshot's worst-case cost. Concurrency safety is proven by
`TestCodeReachabilityProjectionRunnerPartitionsDisjointRepositoriesConcurrently`
(disjoint partitions parallelize, never overlap a partition) and
`TestCodeReachabilityProjectionRunnerSerializesSamePartitionInputs` (same-key
inputs serialize, newest watermark wins). Classification: Scheduling/Wall-clock
win (disjoint partitions parallelize) plus Correctness (bounded, honest
truncation). No-Regression Evidence: the existing single-input runner and
projection tests are unchanged and still pass.

Observability Evidence: the runner logs `code reachability projection completed`
with `partition_count`, `concurrency`, `snapshots_truncated`, `input_count`,
`row_count`, and `duration_seconds`, and emits a `code reachability snapshot
truncated at max visited bound` warning carrying `scope_id`, `generation_id`,
`repository_id`, and `visited` whenever a snapshot hits the bound, so an
operator can see partition fan-out, throughput, and any truncation at 3 AM.
Dead-code candidate paging uses `DeadCodeCandidateRows` in
`content_reader_dead_code_candidates.go:13` when the content read model is
available, pushing the optional language predicate into the Postgres query so
mixed repositories do not fill the bounded page with another language before
policy checks run. When `repo_id` is omitted, the same content-model scan stays
bounded and deterministic by ordering across repository, relative path, entity
name, and entity id instead of returning an empty page. Candidate
hydration then uses `GetEntityContents` in `content_reader_entity.go:49` so
large repo scans merge parser metadata in one bounded content-store read per
candidate page instead of one Postgres round trip per graph row.
The scanner de-duplicates entity IDs across candidate labels before hydration,
so multi-label graph rows do not inflate result counts or content-store reads.
Static TypeScript registry members are reported when parser metadata proves an
exported object registry holds the same-file function value. The analysis
payload names modeled root kinds in `modeled_framework_roots`, reports whether
reflection evidence is modeled, and counts how many suppressions came from
parser metadata. C, C#, C++, Kotlin, Scala, Elixir, Perl, PHP, Ruby, and Groovy
root suppressions are tested through both graph-shaped rows and content-store
metadata so the policy matches the normal hydrated read path.
That lets MCP and CLI callers explain why a candidate was suppressed. Candidate
reads remain label-scoped and are repo-anchored when the request supplies a
repository id, then content-backed policy checks run before completed reducer
code-call and inheritance intent rows are checked for incoming edges.
Content-backed incoming-edge checks group candidates by repository before
calling the relational read model so repo-optional scans do not ask one
repository for another repository's entity ids. Exact one-entity graph probes
are avoided: `deadCodeResultsWithGraphIncomingEdges` in
`code_dead_code_scan.go:258` batches candidate ids into one graph read for
content stores without that relational read model and for SQL routine
reachability, whose reducer-owned `EXECUTES` edges are graph-written rather
than stored as completed shared-projection intent rows. Small display limits use
a bounded 2,500-row scan window per selected candidate label, so a narrow MCP request does not become
incomplete just because most raw candidates are later suppressed. The response
separates display truncation from bounded raw candidate-scan truncation so
callers know whether the returned page was clipped or a per-label scan window was
exhausted.

## Operational invariants

- `code_quality.dead_code` is a derived query unless the language maturity row
  says otherwise. Handler changes must preserve `classification`,
  `dead_code_language_maturity`, and `analysis` fields so MCP and CLI callers
  can distinguish actionable unused symbols from excluded or ambiguous ones.
  Go root-kind evidence covers function roots and type roots, including
  `go.dependency_injection_callback`, `go.direct_method_call`,
  `go.fmt_stringer_method`, `go.function_literal_reachable_call`,
  `go.function_value_reference`, `go.generic_constraint_method`,
  `go.imported_direct_method_call`, `go.imported_fmt_stringer_method`,
  `go.interface_implementation_type`, `go.interface_method_implementation`,
  `go.interface_type_reference`, `go.method_value_reference`, and
  `go.type_reference`. JavaScript-family
  analysis must list Node package, CommonJS default export, CommonJS mixin,
  Next.js, Node migration, Hapi-style, TypeScript public API, TypeScript
  module-contract, and TypeScript interface implementation roots, plus Java
  main, constructor, override, Ant `Task` setter, Gradle plugin `apply`, task
  action/property, and public Gradle DSL roots when query policy suppresses
  those candidates, plus Swift parser-backed roots when query policy suppresses
  those candidates; the analysis notes name the same Java and C root families.
  Rust parser-backed root,
  syntax-evidence, and observed-blocker rows must stay aligned with the
  `deadCodeLanguageMaturity` table because Rust derived classification depends
  on that maturity row, the root suppression policy, and ambiguous
  classification for exactness-blocked candidates.
  The handler scans raw content-model or graph candidates in bounded
  label-scoped pages before policy exclusions, pushes any requested language
  filter into the candidate query, then checks completed reducer code-call
  intent rows for incoming edges on the remaining candidates and uses a
  2,500-row per-label scan window for small result limits. It reports the
  aggregate maximum as `candidate_scan_limit` and the per-label bound as
  `candidate_scan_limit_per_label`, plus
  `candidate_scan_pages` plus `candidate_scan_rows`.
  `display_truncated` and `candidate_scan_truncated` must stay separate so
  performance bounds do not blur result-list pagination with raw scan coverage.
  Unsupported language metadata and repository-root
  `test/`, `tests/`, and `__tests__/` paths stay out of default cleanup results.
