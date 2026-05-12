# Dead Code Reachability Spec

This document defines the reachability model required before Eshu can claim an
`exact` dead-code answer.

## Why This Exists

"No incoming calls" is not the same as "dead."

Frameworks, workers, routers, reflection, and configuration-driven dispatch all
create valid reachability roots that do not look like ordinary direct calls.

Eshu must not claim authoritative dead-code truth until those roots are modeled
for the relevant language and framework.

## Root Categories

Every dead-code analysis must classify roots into one or more of these groups:

- language entrypoints
  - `main`
  - `__main__`
  - Python functions called from `if __name__ == "__main__"` script guards
  - `init()` or equivalent initializer hooks
  - equivalent executable roots
- CLI command roots
  - Cobra commands
  - Click/Typer commands
  - equivalent command registrations
  - Go direct Cobra run-signature handlers are currently modeled as derived
    roots via `*cobra.Command` + `[]string` function signatures
  - Go direct Cobra `Run` / `RunE` registrations are also currently modeled as
    derived roots when the Go parser sees `cobra.Command{Run|RunE: fn}` or a
    proven `cmd.Run|RunE = fn` assignment in the same file
- HTTP and RPC roots
  - route handlers
  - FastAPI/Django/Flask registrations
  - gRPC service handlers
  - Go stdlib HTTP handlers are currently modeled as derived roots via
    `http.ResponseWriter` + `*http.Request` function signatures
  - Go stdlib HTTP registrations are also currently modeled as derived roots
    when the Go parser sees direct `http.HandleFunc`, `http.Handle`, or a
    proven `ServeMux` registration in the same file
- background worker roots
  - Celery tasks
  - Sidekiq jobs
  - cron and scheduler registrations
- framework callback roots
  - Kubernetes admission webhooks
  - controller-runtime reconciler methods
  - ArgoCD/Crossplane hook registrations
  - Go controller-runtime reconciler callbacks are currently modeled as
    derived roots via `Reconcile(context.Context, ctrl|reconcile.Request)
    (ctrl|reconcile.Result, error)` signatures
- generated and tool-owned roots
  - gRPC stubs
  - sqlc output
  - protobuf/OpenAPI generated clients where configured
- SQL and stored-program roots
  - SQL routines explicitly invoked from application code or runtime wiring
  - SQL trigger routines reached through parser-proven trigger-to-function
    `EXECUTES` edges
- reflection and dynamic-dispatch roots
  - explicit allowlisted reflection or registry patterns
- library public API roots
  - exported symbols in library-mode packages
  - per-language public-surface rules
  - Go (currently modeled, default-on): Functions, Structs, Interfaces, and
    Classes whose first rune is uppercase are public-API roots when the file
    path is outside `cmd/`, `internal/`, and `vendor/` subtrees. Binary
    entrypoints (`cmd/`) and internal packages (`internal/`) remain subject
    to reachability rules.
  - Python (currently modeled, bounded): same-module `__all__` entries and
    names re-exported from package `__init__.py` are public-API roots. Public
    methods on those parser-proven public classes are also roots. Base classes
    inherited by parser-proven public classes are treated as public API bases
    so inherited methods are protected. Eshu does not treat every
    non-underscore Python symbol as public in application code.
  - C++ (currently modeled, bounded): functions and class methods declared in
    directly included local headers are public-API roots. Eshu does not yet
    resolve transitive include graphs or target-specific public surfaces.
  - C# does not yet claim a broad public API surface. ASP.NET controller actions,
    hosted-service callbacks, constructors, overrides, same-file interface
    methods and implementations, test methods, serialization callbacks, and
    `Main` are modeled as derived roots.
  - PHP does not yet claim a broad Composer or package public API surface.
    Script entrypoints, constructors, known magic methods, same-file interface
    methods and implementations, trait methods, route-backed controller
    actions, literal route handlers, Symfony route attributes, and WordPress
    hook callbacks are modeled as derived roots.
  - Ruby does not yet claim a broad public API surface. Rails controller
    actions are modeled as framework roots, not general library exports.
  - Rust, Java, and broader language-specific public-surface rules remain
    Chunk 4 follow-up work.
- conditional roots
  - build-tag, platform, or environment-specific reachability
- user-declared roots
  - repository-specific overrides in configuration

## Exactness Rule

Dead-code truth is `exact` only when:

1. the language/framework root model is implemented for the target scope
2. the authoritative call graph is present
3. the runtime can explain which root categories were applied

If those conditions are not met, Eshu must either:

- return `derived`, or
- reject the request as unsupported for that profile

It must not pretend a partial root model is authoritative.

## Mandatory Language Contract

Every parser-supported source language has the same exactness contract:

- if the language can define code, Eshu must model those definitions for the
  candidate kinds it reports
- if the language can reference, call, import, inherit, implement, decorate,
  annotate, export, register, or otherwise make code reachable, Eshu must model
  that syntax or semantic path before claiming exact dead-code truth
- if the language has standard package, module, workspace, build, feature, or
  target-selection rules, Eshu must resolve them or return a named exactness
  blocker
- if the language has runtime or toolchain expansion that can create reachable
  symbols, such as macros, annotation processors, code generation, plugin
  registries, reflection, metaprogramming, or framework auto-discovery, Eshu
  must model the expansion or return a named exactness blocker
- if a construct is valid in the language and can affect dead-code reachability,
  it cannot be silently ignored in an exact result

This contract applies per language. A response may return `exact` only for the
repo or query scope whose observed evidence satisfies that language contract.
When a scope contains valid but unsupported language behavior, the response must
return `derived`, `derived_candidate_only`, `ambiguous_only`, or an unsupported
exactness state with explicit blocker names such as `macro_expansion_unavailable`,
`cfg_unresolved`, `dynamic_import_unresolved`, or `reflection_unresolved`.

Parser support is therefore the floor, not the finish line. The parser and
dead-code dogfood tickets for each language must track the full language
surface that can affect reachability, not only the constructs currently visible
in fixtures.

## Required Output Metadata

Any dead-code result should be able to report:

- root categories used
- frameworks recognized
- dead-code maturity for each parser-supported source language
- whether reflection/dynamic patterns were modeled
- whether tests or generated code were excluded
- applied user overrides
- how many candidate entities skipped framework-root evaluation because source
  text was unavailable
- how many framework roots came from parser metadata versus legacy query-time
  source fallback
- named exactness blockers for parser-supported languages that are modeled but
  still non-exact
- whether IaC reachability was modeled by this analysis

That explanation must be returned in structured form, not just text prose.

Language maturity is separate from parser support. A parser-supported language
can still be `derived_candidate_only` for dead-code cleanup until it has a
dead-code fixture suite, root model, reachability proof, and API/MCP evidence.
The initial maturity states are:

- `derived`: current C, C#, C++, Go, PHP, Python, Java, JavaScript, TypeScript,
  TSX, Ruby, Rust, and SQL candidate scans with partial root modeling
- `derived_candidate_only`: parser-supported source languages where Eshu can
  return graph-backed candidates but has not implemented enough language roots
  and fixtures for cleanup-safe answers
- `unsupported_language`: languages outside Eshu's parser/indexing contract for
  this capability
- future `exact`: language scopes whose fixture and runtime gates prove the
  answer is cleanup-safe
- future `ambiguous_only`: language scopes where Eshu can identify uncertainty
  but cannot return actionable unused symbols

Rust currently reports `derived` with named exactness blockers for unresolved
macro expansion, cfg and Cargo feature selection, semantic module resolution,
and trait dispatch. Those blockers must be cleared or scoped out before Rust
can return exact cleanup-safe dead-code truth.

SQL currently reports `derived` for stored routine cleanup. `SqlFunction`
candidates participate in `code_quality.dead_code`, and trigger routines are
protected when reducer materialization creates trigger-to-function `EXECUTES`
edges from parsed `sql_relationships`. SQL remains non-exact until dynamic SQL,
dialect-specific routine resolution, and migration-order resolution are modeled
or scoped out.

Groovy currently reports `derived_candidate_only`. Jenkinsfile pipeline
entrypoints and Jenkins shared-library `vars/*.groovy` `call` methods are
modeled as parser-backed roots, but Groovy dynamic dispatch, closure delegates,
Jenkins shared-library loading, and pipeline DSL dynamic steps remain named
exactness blockers.

C currently reports `derived` with parser-backed roots for `main`, functions
declared by directly included local headers, signal handlers, callback
arguments, and direct function-pointer initializer targets. It remains non-exact
until macro expansion, conditional compilation, build-target selection,
transitive include graphs, broader callback registration, dynamic symbol lookup,
and external-linkage resolution are modeled or scoped out.

C++ currently reports `derived` with parser-backed roots for `main`, functions
and methods declared by directly included local headers, virtual methods,
override methods, direct callback arguments, and direct function-pointer
initializer targets, plus Node native-addon entrypoint macros such as
`NAPI_MODULE_INIT`. It remains non-exact until broader macro expansion,
conditional compilation, build-target selection, transitive include graphs,
template instantiation, overload resolution, virtual dispatch breadth, broader
callback registration, dynamic symbol lookup, and external-linkage resolution
are modeled or scoped out.

C# currently reports `derived` with parser-backed roots for `Main`,
constructors, overrides, same-file interface methods and implementations,
ASP.NET controller actions, hosted-service callbacks, test methods, and
serialization callbacks. It remains non-exact until reflection, dependency
injection, source-generator output, partial type resolution, dynamic dispatch,
project references, and broad public API surfaces are modeled or scoped out.

Kotlin currently reports `derived` with parser-backed roots for top-level
`main`, secondary constructors, interface methods, same-file interface
implementations, overrides, Gradle plugin and task callbacks, Spring component
and method callbacks, lifecycle callbacks, and JUnit methods. It remains
non-exact until reflection, dependency injection, annotation processing,
compiler plugin output, dynamic dispatch, Gradle source-set resolution,
multiplatform target resolution, and broad public API surfaces are modeled or
scoped out.

PHP currently reports `derived` with parser-backed roots for script
entrypoints, constructors, known magic methods, same-file interface methods and
implementations, trait methods, route-backed controller actions, literal route
handlers, Symfony route attributes, and WordPress hook callbacks. It remains non-exact
until dynamic dispatch, reflection, Composer/autoload public surfaces,
include/require resolution, broader framework routes, trait resolution,
namespace alias breadth, magic-method dispatch, and broad public API surfaces
are modeled or scoped out.

Ruby currently reports `derived` with parser-backed roots for Rails controller
actions, Rails callback methods declared by literal callback symbols, literal
method-reference targets, `method_missing` / `respond_to_missing?` dynamic
dispatch hooks, and functions called from `__FILE__ == $PROGRAM_NAME` script
guards. It remains non-exact until broader metaprogramming, autoload and
constant resolution, framework route files, and gem public API surfaces are
modeled or scoped out.

The current `code_quality.dead_code` capability is code-call oriented. It must
not classify Dockerfiles, Terraform, Helm, Kustomize, Kubernetes, ArgoCD, or
other IaC/runtime artifacts as dead simply because they lack code-call inbound
edges. Dead-IaC requires the separate IaC usage and reachability graph described in
`../adrs/2026-04-24-iac-usage-reachability-and-refactor-impact.md`.

## Default Scope Policy

- tests are excluded from dead-code roots by default
- generated code is excluded by default unless the repo explicitly opts in
- library-mode exported symbols are roots by default unless a stricter rule is
  configured
- Go `init()` reachability follows import side effects; symbols made reachable
  only by side-effect imports are not dead if that import path is active

## User Overrides

Repos may declare additional roots or exclusions in `.eshu.yaml`.

Initial keys:

```yaml
dead_code:
  roots: []
  exclude_paths: []
  include_generated: false
```

Generated code detection should begin with:

- standard generated-file headers
- common generated path patterns such as `gen/`, `generated/`, and tool-owned
  output directories

## Deliverable For Chunk 4

Chunk 4 must produce a framework-aware root registry or rules layer that is
explicitly testable per language and framework family.

Minimum initial coverage should include:

- Go CLI/HTTP/controller patterns
- Python web and worker patterns
- JavaScript/TypeScript web route patterns

Current branch status:

- Go direct Cobra run signatures are modeled
- Go direct Cobra `Run` / `RunE` registrations are modeled
- Go stdlib HTTP handler signatures are modeled
- Go stdlib HTTP direct and proven `ServeMux` registrations are modeled
- Go controller-runtime `Reconcile` signatures are modeled
- Go composite-literal type references are modeled as `REFERENCES` edges, not
  `CALLS`, so type usage can protect struct/interface candidates without
  claiming invocation semantics
- Python FastAPI route decorators are modeled
- Python Flask route decorators are modeled
- Python Celery task decorators are modeled
- Python Click and Typer command decorators are modeled
- Python AWS Lambda handlers declared in bounded SAM/CloudFormation and
  Serverless config files are modeled
- Python class-method context and simple local constructor receiver calls are
  modeled so same-file `Class.method` and `instance.method` calls can reach
  methods without broad name guessing
- Python constructor calls, inherited classmethod calls, class receiver
  references, dataclass models, dataclass `__post_init__` hooks, and property
  decorators are modeled conservatively
- Python dunder protocol methods are modeled as runtime callback roots
- Python same-module `__all__` exports and package `__init__.py` reexports are
  modeled as bounded public-API roots, and public base classes plus public
  methods on those parser-proven public classes are modeled without marking every
  public-looking Python symbol live
- JavaScript/TypeScript Next.js route exports are modeled
- JavaScript/TypeScript Express handler registrations are modeled
- JavaScript/TypeScript Node package entrypoints, package `bin` targets,
  package public exports, and exported functions under configured
  Hapi/lib-api-hapi handler directories are modeled when `package.json` and
  `server/init/plugins/spec*` provide bounded local evidence
- TypeScript public methods on classes that declare `implements` are modeled as
  interface implementation method roots; private and protected class helpers
  remain candidates unless another root or incoming edge reaches them
- C main functions, directly included public-header declarations, signal
  handlers, callback arguments, and direct function-pointer initializer targets
  are modeled as parser-backed roots
- C++ main functions, directly included public-header declarations, virtual and
  override methods, callback arguments, and direct function-pointer initializer
  targets plus Node native-addon entrypoints are modeled as parser-backed roots
- Ruby Rails controller actions, Rails callback methods, dynamic-dispatch hooks,
  literal method-reference targets, and script entrypoints are modeled as
  parser-backed roots
- Groovy Jenkinsfile pipeline entrypoints and Jenkins shared-library
  `vars/*.groovy` `call` methods are modeled as parser-backed roots
- Java main methods, constructors, overrides, Spring/JUnit/Jenkins/Stapler
  callbacks, Gradle plugin/task surfaces, serialization and Externalizable
  hook signatures, bounded literal reflection, ServiceLoader providers, Spring
  Boot `AutoConfiguration.imports`, and legacy `spring.factories` metadata are
  modeled with parser-backed roots or reducer-produced `REFERENCES` edges
- Kotlin top-level main functions, secondary constructors, interfaces,
  overrides, Gradle plugin/task callbacks, Spring component and method
  callbacks, lifecycle callbacks, and JUnit methods are modeled as
  parser-backed roots
- those Go signature roots are now emitted by the Go parser into entity
  metadata when imports, registrations, and signatures match directly; mixed
  native+SCIP indexing now preserves `dead_code_root_kinds` through the
  supplement merge path; Python route/task/CLI decorators, AWS Lambda handler
  config roots, dataclass/property roots, dunder protocol roots, bounded
  public-API roots, bases, and members, and JavaScript/TypeScript
  Next.js/Express/Node/Hapi plus TypeScript interface implementation method
  roots are also emitted as parser-backed `dead_code_root_kinds`; Java metadata
  and literal reflection evidence now materializes as `REFERENCES` edges; Go
  query-time source heuristics remain as a fallback while broader registry
  coverage lands; C parser roots cover bounded entrypoint, header, and callback
  evidence without scanning every repository header; Rust Cargo entrypoints,
  tests, Tokio runtime/test functions,
  exact `pub` public API items, Criterion benchmark functions, direct trait impl
  methods, Cargo auxiliary-target exclusions, conditional derive evidence,
  nested annotations, structured where-clause evidence, path-attribute modules,
  direct file module-resolution status, and literal macro body module/import
  declarations are modeled as parser-backed derived evidence; SQL
  `SqlFunction` routines are scanned as derived candidates, and parser-proven
  trigger-to-function `EXECUTES` edges protect trigger-invoked routines from
  cleanup results
- broader Go router, webhook, worker, reflection, and build-tag roots plus
  broader Python worker, dynamic-dispatch, and non-export-declared public API
  roots plus broader
  JavaScript/TypeScript worker, static module graph, and dynamic-dispatch roots
  plus broader Java dynamic dispatch, dependency injection, and string-built
  reflection plus C macro expansion, conditional compilation, transitive include
  graphs, dynamic symbol lookup, and broader callback registries plus C++ macro
  expansion, conditional compilation, transitive include graphs, template
  instantiation, overload resolution, virtual dispatch breadth, and broader
  callback registries plus arbitrary Rust macro expansion, cfg/Cargo feature solving, cross-crate semantic module
  resolution, broad trait dispatch, dynamic SQL, dialect-specific routine
  resolution, and SQL migration-order resolution
  remain open, so dead-code truth stays `derived`

Initial MVP is explicitly limited to those families. Other parser-supported
languages and frameworks should return `derived_candidate_only`, `derived`, or
`ambiguous_only` dead-code results until their root models exist.

Chunk 4 should also add a diff-oriented dead-code mode for CI-style questions
such as "did this change introduce dead code?"

## Dead-Code Fixtures

Each parser-supported source language needs a dedicated dead-code fixture suite
before exactness can be claimed. The parser fixture matrix proves syntax
extraction coverage; it does not prove cleanup safety.

The fixture inventory lives at
`../../../tests/fixtures/deadcode/README.md`.
The per-parser support pages under `../languages/` name the checked fixtures
and root categories for each currently modeled source language.

Every language fixture should include:

- a truly unused symbol that should be reported
- a direct call/reference that should not be reported
- an executable entrypoint or initializer
- an exported/public API surface
- a common framework, router, worker, annotation, decorator, or callback root
- a language-specific semantic dispatch case such as function values, method
  values, interfaces, traits, dynamic imports, or generated registries
- a generated-code or test-owned exclusion
- an ambiguous dynamic case that keeps truth non-exact
- every valid language construct that can affect dead-code reachability, or an
  explicit exactness blocker proving why that construct prevents exact output

Fixture intent must be asserted at three layers when the language supports
them: parser evidence, graph/query classification, and API/MCP/local proof.
A language with parser fixtures but no dead-code fixtures remains `derived` or
`derived_candidate_only` for `code_quality.dead_code`.

## Test Requirements

- positive case: reachable symbol preserved by framework root
- negative case: truly unreachable symbol flagged
- ambiguous case: unresolved dynamic or framework registration forces a
  non-exact answer
