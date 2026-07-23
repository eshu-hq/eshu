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
| Dead-code roots | `dead-code-roots` | derived | `functions.metadata.dead_code_root_kinds` | `name, line_number, dead_code_root_kinds` | `code_quality.dead_code` root suppression | `go/internal/parser/ruby_dead_code_roots_test.go::TestDefaultEngineParsePathRubyEmitsDeadCodeRootKinds`, `go/internal/parser/ruby_dead_code_roots_test.go::TestDefaultEngineParsePathRubyGatesControllerActionOnSuperclassChain`, `go/internal/query/code_dead_code_ruby_roots_test.go::TestHandleDeadCodeExcludesRubyRootKindsFromMetadata` | Compose-backed Ruby dogfood required by issue #93 | Rails callback methods, dynamic dispatch hooks, literal method-reference targets, and script entrypoints are modeled as derived roots. Rails controller-action roots now require the enclosing class to extend a real Rails base (`ApplicationController`, `ActionController::Base`/`API`/`Metal`, or `AbstractController::Base`) resolved through a same-file transitive superclass walk: a class merely named `*Controller` no longer roots unless it declares no superclass at all (conservative keep) or extends an unresolved `*Controller`-suffixed base. Same-file short-name collisions across namespaces are resolved repo-wide by the #5376 reducer `code_root_verdicts` projection: the parser emits `qualified_name` and the reducer re-runs the identical controller decision against a repo-wide, qualified-name-keyed registry, downgrading a `*Controller` only when its real base resolves onward to a curated non-controller Rails terminal (`ActiveRecord::Base`, `ActiveJob::Base`, `ActionMailer::Base`, `ApplicationRecord`, `ApplicationJob`, `ApplicationMailer`). A base that matches an in-corpus class only by a proper namespace-suffix can never drive a downgrade (it could shadow a gem class of the same short name); such a base keeps unless a confirm-only probe reaches a real Rails base. Precision upgrade (#5500, optional, does not change the safety posture): candidate resolution also tries the lexical-prefix names real Ruby constant lookup would try for a base ref seen inside the walked class's own namespace `P` — `P::ref`, then each enclosing prefix of `P`, then top-level `::ref` — alongside the broad, unscoped search, and unions EVERY level's exact hit (it never stops at the first one, since a class's qualified name cannot distinguish a genuinely nested-module-block declaration from Ruby's compact colon form, and stopping early could let a coincidentally same-named inner-scope class mask the true bare referent). This lets a base that previously had only a same-last-segment suffix match resolve EXACTLY when the true, lexically-scoped referent is in-corpus, so it can confirm or downgrade instead of staying `suffix_only_ambiguous`; it never removes a match the prior, unscoped lookup found (the bare ref is always one of the unioned candidates). The verdict `basis.reason` is a closed set: `accepted`, `unresolved_non_controller`, `rejected_framework_base`, `unresolved_qualified`, `unresolved_controller`, `suffix_only_ambiguous`, `fizzled`, `collision`, `cycle`, `depth_cap`. Documented residual (A2): a gem constant defined under an app-authored namespace could be mistaken for an in-corpus class; this is a namespace-hygiene limitation, not a silent failure. |

## Parser Performance

`Parse` walks the tree-sitter AST once per file for dead-code roots and
framework routes instead of four times (issue
[#4842](https://github.com/eshu-hq/eshu/issues/4842), child of epic
[#4831](https://github.com/eshu-hq/eshu/issues/4831)). Previously
`annotateRubyDeadCodeRoots` ran three separate `shared.WalkNamed` passes
(Rails callback registrations, literal method references, script-entrypoint
guards) and `buildRubyFrameworkSemantics` ran its own bespoke recursive walk
for Rails and Sinatra route detection, each re-traversing the same AST. All
four are now collected by one merged `shared.WalkNamed` pass
(`rubyCollectSemantics`): the three dead-code checks are pure node-local
predicates with no interaction, so they run unconditionally for every `call`
node, and route context (which framework/class a route call is nested under)
is resolved by climbing from a candidate call node to its nearest
context-changing ancestor (a class extending `Sinatra::Base`, or a
`Rails.application.routes.draw` call) instead of being threaded down during
descent. The nearest context-changing ancestor found by climbing is exactly
the context top-down threading always assigned, so folding route resolution
into the flat pass does not reorder or alter any of the four original
analyses.

Parser output is byte-identical before and after this change: a corpus dump
across all `.rb` fixtures under `tests/fixtures`, canonicalized to recursively
key-sorted JSON and hashed per file per `Options` variant (`Options{}` and
`Options{IndexSource: true}`), produced a `0/0` symmetric diff between the
pre-change and post-change dumps. This is a one-time manual differential
(`go/internal/parser/ruby/equivalence_dump_test.go`, opt-in via
`RUBY_PARSE_DUMP`), not a standing CI gate; standing protection is the ruby
parser package tests plus the B-12 golden snapshot.

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
