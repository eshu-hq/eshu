# Ruby Parser

This page tracks the checked-in Go parser contract in the current repository state.
Canonical implementation: `go/internal/parser/registry.go` plus the entrypoint and tests listed below.

## Parser Contract
- Language: `ruby`
- Family: `language`
- Parser: `DefaultEngine (ruby)`
- Entrypoint: `go/internal/parser/ruby_language.go`
- Fixture repo: `tests/fixtures/ecosystems/ruby_comprehensive/`
- Unit test suite: `go/internal/parser/engine_ruby_semantics_test.go`
- Integration validation: compose-backed fixture verification (see `../reference/local-testing.md`)

## Capability Checklist
| Capability | ID | Status | Extracted Bucket/Key | Required Fields | Graph Surface | Unit Coverage | Integration Coverage | Rationale |
|-----------|----|--------|------------------------|-----------------|---------------|---------------|----------------------|-----------|
| Methods (`def`) | `methods-def` | supported | `functions` | `name, line_number` | `node:Function` | `go/internal/parser/engine_long_tail_test.go::TestDefaultEngineParsePathRubyFixtures` | Compose-backed fixture verification | - |
| Classes | `classes` | supported | `classes` | `name, line_number` | `node:Class` | `go/internal/parser/engine_long_tail_test.go::TestDefaultEngineParsePathRubyFixtures` | Compose-backed fixture verification | - |
| Modules | `modules` | supported | `modules` | `name, line_number` | `node:Module` | `go/internal/parser/engine_long_tail_test.go::TestDefaultEngineParsePathRubyFixtures` | Compose-backed fixture verification | - |
| Require/load imports | `require-load-imports` | supported | `imports` | `name, line_number` | `relationship:IMPORTS` | `go/internal/parser/engine_ruby_semantics_test.go::TestDefaultEngineParsePathRubyEmitsRequireAndLoadImports` | Compose-backed fixture verification | - |
| Method calls | `method-calls` | supported | `function_calls` | `name, line_number` | `relationship:CALLS` | `go/internal/parser/engine_ruby_semantics_test.go::TestDefaultEngineParsePathRubyCapturesGenericDslAndMethodCalls` | Compose-backed fixture verification | - |
| Instance variable assignments | `instance-variable-assignments` | supported | `variables` | `name, line_number` | `node:Variable` | `go/internal/parser/engine_ruby_semantics_test.go::TestDefaultEngineParsePathRubyCapturesLocalAndInstanceAssignments` | Compose-backed fixture verification | - |
| Local variable assignments | `local-variable-assignments` | supported | `variables` | `name, line_number` | `node:Variable` | `go/internal/parser/engine_ruby_semantics_test.go::TestDefaultEngineParsePathRubyCapturesLocalAndInstanceAssignments` | Compose-backed fixture verification | - |
| Module inclusions (`include`/`extend`) | `module-inclusions-include-extend` | supported | `module_inclusions` | `class, module, line_number` | `relationship:INCLUDES` | `go/internal/parser/engine_long_tail_test.go::TestDefaultEngineParsePathRubyFixtures` | Compose-backed fixture verification | - |
| Parent context (class/module) | `parent-context-class-module` | supported | `functions` | `name, line_number, class_context` | `property:Function.class_context` | `go/internal/parser/engine_ruby_semantics_test.go::TestDefaultEngineParsePathRubyEmitsFunctionArgsAndContext` | Compose-backed fixture verification | - |
| Exact Rails/Sinatra route entries | `rails-sinatra-literal-route-truth` | supported | `framework_semantics.{rails,sinatra}.route_entries` | `method, path, handler` | `relationship:HANDLES_ROUTE` when the reducer resolves one exact handler | `go/internal/parser/ruby_route_entries_test.go::TestDefaultEngineParsePathRubyEmitsExactRailsRouteEntries`, `go/internal/parser/ruby_route_entries_test.go::TestDefaultEngineParsePathRubyEmitsExactSinatraMethodRouteEntries`, `go/internal/reducer/handles_route_ruby_test.go::TestBuildHandlesRouteIntentRowsEmitsRubyRailsRouteMatches`, `go/internal/query/content_reader_framework_routes_ruby_test.go::TestParseFrameworkSemanticsExtractsRubyRoutes` | Golden corpus gate | Exact literal Rails `to: "controller#action"` entries inside `Rails.application.routes.draw` and Sinatra `&method(:handler)` route blocks emit route entries. Reducer projection stays exact-only and skips unresolved or ambiguous handlers. |
| Dead-code roots | `dead-code-roots` | derived | `functions.metadata.dead_code_root_kinds` | `name, line_number, dead_code_root_kinds` | `code_quality.dead_code` root suppression | `go/internal/parser/ruby_dead_code_roots_test.go::TestDefaultEngineParsePathRubyEmitsDeadCodeRootKinds`, `go/internal/query/code_dead_code_ruby_roots_test.go::TestHandleDeadCodeExcludesRubyRootKindsFromMetadata` | Compose-backed Ruby dogfood required by issue #93 | Rails controller actions, Rails callback methods, dynamic dispatch hooks, literal method-reference targets, and script entrypoints are modeled as derived roots. |

## Framework And Library Support

Supported today:

- Rails controller actions and Rails callback methods are modeled as derived
  roots when the parser sees the source shape.
- Literal Rails route entries inside `Rails.application.routes.draw` are emitted
  when the HTTP method, path, and `to: "controller#action"` target are all
  source literals. The parser normalizes the controller string to the matching
  `ControllerClass.action` handler name, and the reducer only projects
  `HANDLES_ROUTE` when that handler resolves to one indexed function.
- Literal Sinatra route entries are emitted for source-proven named handler
  blocks such as `get "/health", &method(:health)` when the parser sees a
  Sinatra import or `Sinatra::Base` subclass. Anonymous route blocks are not
  treated as named handler truth.
- Literal method-reference targets, `method_missing`, `respond_to_missing?`,
  and script guards are protected as runtime root evidence.

Not claimed today:

- Dynamic Rails route paths, dynamic `to:` targets, namespaced controller route
  strings, Rails autoload/eager-load behavior, anonymous Sinatra blocks,
  generated route files, ActiveRecord scopes, gem public API surfaces, broad
  constant resolution, and broader metaprogramming remain outside the exactness
  boundary.
- Exact route-to-handler truth for Ruby frameworks beyond the literal Rails and
  named Sinatra subset is not claimed.

## Known Limitations
- Singleton methods on specific objects are only separated for `def self.name`
  and `class << self`; broader singleton-object targets are not resolved.
- `method_missing` and `respond_to_missing?` are protected as runtime hooks, but
  arbitrary dynamic dispatch targets are not resolved.
- Rails autoload/eager-load configuration, namespaced controller string
  resolution, anonymous Sinatra route blocks, ActiveRecord scopes, gem public
  API surfaces, and broad constant resolution remain exactness blockers.
