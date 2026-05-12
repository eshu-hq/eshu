# Dead-Code Fixture Corpus

This corpus is for `code_quality.dead_code` maturity. It is separate from the
general parser fixtures because parser coverage proves syntax extraction, not
cleanup safety.

Every parser-supported source language needs a fixture row before Eshu can
claim exact dead-code behavior for that language. A language may still return
graph-backed candidates before exactness, but its maturity remains
`derived_candidate_only` or `derived` until the fixture and root-model gates
below pass.

## Required Cases

Each language fixture must include:

- `unused`: one symbol that should be returned as dead-code
- `direct_reference`: one symbol reached by a direct call/import/reference
- `entrypoint`: executable entrypoint, initializer, or module root
- `public_api`: exported or public surface that should not be treated as dead
- `framework_root`: route, command, worker, annotation, decorator, callback, or
  equivalent ecosystem root
- `semantic_dispatch`: function value, method value, interface, trait,
  dynamic import, generated registry, or equivalent language dispatch
- `excluded`: generated code or test-owned code excluded by default
- `ambiguous`: dynamic case that must keep truth non-exact

## Maturity States

| State | Meaning |
| --- | --- |
| `derived_candidate_only` | Parser can index the language and Eshu can return graph-backed candidates, but root fixtures are not complete. |
| `derived` | Some root categories are modeled and tested, but exact cleanup is not proven. |
| `ambiguous_only` | Eshu can identify uncertainty but should not return cleanup candidates for the language scope. |
| `exact` | Fixture, root, reachability, backend, API, MCP, and CLI gates prove cleanup-safe answers for the language scope. |

## Language Inventory

| Language | Fixture status | Initial maturity | Required focus |
| --- | --- | --- | --- |
| C | active | `derived` | transitive include graphs, build-target conditionals, broader callback registries |
| C# | active | `derived` | public API surfaces, dependency injection, reflection, source generators, partial types |
| C++ | active | `derived` | transitive include graphs, templates, overload resolution, build-target conditionals |
| Dart | planned | `derived_candidate_only` | public libraries, Flutter callbacks, constructors |
| Elixir | planned | `derived_candidate_only` | modules, behaviours, Phoenix roots, supervision callbacks |
| Go | active | `derived` | function values, local interfaces, method sets, DI callbacks |
| Groovy | planned | `derived_candidate_only` | Jenkins pipeline entrypoints and shared-library calls |
| Haskell | planned | `derived_candidate_only` | module exports, typeclasses, executable entrypoints |
| Java | planned | `derived_candidate_only` | public APIs, interface methods, annotations, Spring roots |
| JavaScript | active | `derived` | module exports, Express/Next.js roots, dynamic property ambiguity |
| Kotlin | planned | `derived_candidate_only` | public APIs, interfaces, annotations, Spring/Ktor roots |
| Perl | planned | `derived_candidate_only` | packages, exported subs, dynamic symbol ambiguity |
| PHP | active | `derived` | Composer/autoload surfaces, dynamic dispatch, reflection, broader framework routes |
| Python | active | `derived` | Lambda roots, bounded public APIs, dataclasses/properties, dynamic imports |
| Ruby | active | `derived` | broader Rails route resolution, metaprogramming, autoload, gem public API |
| Rust | planned | `derived_candidate_only` | public items, traits, macro-generated roots |
| Scala | planned | `derived_candidate_only` | public APIs, traits, annotations, framework roots |
| Swift | active | `derived` | public APIs, macros, SwiftPM targets, protocol witnesses, dynamic dispatch |
| TSX | active | `derived` | React/Next.js roots, component exports, hook ambiguity |
| TypeScript | active | `derived` | exports, Express/Next.js roots, decorators, dynamic imports |

## Promotion Rule

Exactness is language scoped. Promoting one language does not promote the whole
dead-code capability. Promotion requires:

1. Checked-in fixture cases for the language.
2. Parser or SCIP evidence for definitions, calls, references, and root hints.
3. Query tests for `unused`, reachable, excluded, and ambiguous results.
4. API/MCP output proving maturity metadata and truth labels.
5. Backend conformance for NornicDB and Neo4j query shapes.

## Parallel Ownership

Subagents may work on separate language fixture directories in parallel. Shared
parser files must have a single owner at a time. JavaScript, TypeScript, and TSX
share parser code, so they should be assigned to one JS-family worker unless
the task is fixture-only.
