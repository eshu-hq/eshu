# Ruby Parser Audit

## Overview
The Ruby parser (`go/internal/parser/ruby/`) is a tree-sitter-backed adapter that extracts modules, classes, singleton classes, methods, imports (`require`/`require_relative`/`load`), module inclusions (`include`), variables (constants, identifiers, instance variables), method calls (dotted and receiverless), block end lines, Rails-idiomatic dead-code root metadata, and Bundler dependency evidence from `Gemfile`/`Gemfile.lock`. The AST walk in `syntax.go` uses a scope stack for context resolution. Call extraction uses `calls.go` for dotted name composition. The package has 3 subdirectory test files plus 6 parent-level dead-code root tests and a parent-level Ruby semantics test.

## Claimed Constructs
List every construct the parser claims to extract, with source references.

1. **Modules** — `syntax.go:136-155` (`module`)
2. **Classes** — `syntax.go:157-182` (`class`)
3. **Singleton classes** — `syntax.go:184-199` (`singleton_class`)
4. **Methods** — `syntax.go:201-250` (`method`, `singleton_method`)
5. **Function types** — `syntax.go:209-214` (instance, singleton, dynamic_dispatch)
6. **Variables** — `syntax.go:282-306` (constant, identifier, instance_variable)
7. **Imports** — `syntax.go:255-268` (`require`, `require_relative`, `load`)
8. **Module inclusions** — `syntax.go:269-278` (`include`)
9. **Function calls** — `calls.go:19-39` (`call` nodes, dotted full name), `calls.go:44-59` (assignment-side bare identifier calls)
10. **Dead-code root kinds** (`dead_code_roots.go:43-62`, `:66-83`):
    - `ruby.rails_controller_action` (`dead_code_roots.go:54-59`)
    - `ruby.rails_callback_method` (`dead_code_roots.go:67-69`)
    - `ruby.dynamic_dispatch_hook` (`dead_code_roots.go:51-52`)
    - `ruby.method_reference_target` (`dead_code_roots.go:71-74`)
    - `ruby.script_entrypoint` (`dead_code_roots.go:76-79`)
    - Rails callback methods: `before_action`, `after_action`, `around_action`, `before_filter`, `after_filter`, `around_filter` (`dead_code_roots.go:23-30`)
    - Reflection methods: `method`, `send`, `public_send` (`dead_code_roots.go:35-38`)
11. **Cyclomatic complexity** — `complexity.go:38-39`
12. **Class superclass** — `syntax.go:171-174` (`bases` field)
13. **Method arguments** — `syntax.go:223`, `nodes.go:82-104`
14. **Visibility tracking** — `syntax.go:228-229`, `nodes.go:22-29` (public/private/protected)
15. **Context/class_context on functions** — `syntax.go:231-237`
16. **Bundler Gemfile dependencies** — `bundler_gemfile.go:25-57` (groups, source types, version requirements)
17. **Bundler lockfile dependencies** — `bundler_lockfile.go:26-59` (exact versions, dependency paths, direct/transitive)
18. **PreScan** — `parser.go:89-97`

## Verified-by-Test Constructs
List constructs verified by tests, with file:function references.

1. **Modules** — `ruby/parser_test.go:36` (`TestParseCapturesRubyContextAndCalls`)
2. **Classes with bases** — `ruby/parser_test.go:37-40` (Worker with BaseWorker)
3. **Singleton method type** — `ruby/parser_test.go:89-91` (`TestParseCapturesConstantsAndKeepsContextAcrossNestedBlocks`)
4. **Class context on functions** — `ruby/parser_test.go:48-50` (Worker context)
5. **Context_type on variables** — `ruby/parser_test.go:85-87` (class), `:98-103` (def)
6. **Imports** — `ruby/parser_test.go:35` (require_relative)
7. **Module inclusions** — `ruby/parser_test.go:52` (include Cacheable)
8. **Dotted function calls** — `ruby/parser_test.go:53-54` (task.call, Rails.application.routes.draw, env.ready?)
9. **Method arguments** — `ruby/parser_test.go:45-46` (task, retries)
10. **IndexSource** — `ruby/parser_test.go:42-43` (source line capture)
11. **Rails controller action root** — `ruby_dead_code_roots_test.go:75` (`TestDefaultEngineParsePathRubyEmitsDeadCodeRootKinds`)
12. **Rails callback method root** — `ruby_dead_code_roots_test.go:76`
13. **Dynamic dispatch hook root** — `ruby_dead_code_roots_test.go:77-78`
14. **Script entrypoint root** — `ruby_dead_code_roots_test.go:79`
15. **Method reference target root** — `ruby_dead_code_roots_test.go:80`
16. **Dead code fixture expected roots** — `ruby_dead_code_roots_test.go:86-108` (comprehensive fixture)
17. **Receiverless helper calls** — `ruby_dead_code_roots_test.go:112-168` (`TestDefaultEngineParsePathRubyEmitsReceiverlessHelperCalls`)
18. **Array callback methods** — `ruby_dead_code_roots_test.go:173-206` (`before_action [:a, :b]`)
19. **Non-equality script guard rejected** — `ruby_dead_code_roots_test.go:209-246` (`TestDefaultEngineParsePathRubyRejectsNonEqualityScriptGuard`)
20. **Bundler Gemfile dependencies** — `ruby/bundler_test.go:13-59` (direct deps, groups, sources)
21. **Bundler lockfile dependencies** — `ruby/bundler_test.go:63-105` (exact versions, dependency paths)
22. **Git source in lockfile** — `ruby/bundler_test.go:111-145`
23. **CRLF line endings** — `ruby/bundler_test.go:148-161`
24. **Nested group/block end balancing** — `ruby/bundler_test.go:168-185`
25. **Cyclomatic complexity** — `engine_cyclomatic_complexity_test.go:167-181` (2 test cases)
26. **Long-tail comprehensive fixture** — `engine_long_tail_test.go:12-15` (`TestDefaultEngineParsePathRubyFixtures`)

## Unverified / Claimed-but-Untested Constructs
List constructs claimed but not covered by any test.

1. **Singleton class (`class << self`)** — `syntax.go:184-199`: not tested in any test file. Neither the singleton class scope nor methods inside it are explicitly verified.
2. **`def self.name` singleton method** (`syntax.go:211`): tested indirectly via `OrdersController.self.call`, but not isolated.
3. **Visibility transitions** (`public`/`private`/`protected` toggles) — `syntax.go:228-229`: the public/private interaction with Rails controller action marking is tested, but visibility transitions within a class body are not.
4. **`method_missing` / `respond_to_missing?` as dynamic_dispatch_hook** — `dead_code_roots.go:51-52`: tested via dead_code_roots_test but not as a standalone function type test.
5. **Assignment-side bare identifier calls** (`calls.go:44-59`): the `x = build_scopes` pattern is not tested in isolation; only dotted calls from the main test cover call extraction.
6. **Call deduplication by full name + line** (`calls.go:64-66`): not explicitly tested with duplicate calls on the same line.
7. **Variable deduplication across scopes** — `syntax.go:318-321` (`seenVariables`): not tested with a variable assigned in two scopes.
8. **`rubyNormalizeArgument` edge cases** — `calls.go:185-207`: not tested with splat, block, keyword, or quoted arguments.
9. **`rubyInferAssignmentType`** — `calls.go:167-181`: not tested with `new ` prefix stripping or terminal handling.
10. **Opaque block balancing in Bundler** (`bundler_blocks.go`): tested implicitly through group tests, but not in isolation.
11. **Bundler `github:` source type** (`bundler_gemfile.go:16`): no test with `github "user/repo" do`.
12. **Bundler `source` option within group context** (`bundler_gemfile.go:99-103`): not tested.
13. **`Gemfile.lock` with `PATH` section** — `bundler_lockfile.go:139-142`: not tested.

## Edge Cases Considered
List edge cases the tests actually cover with test references.

- **Scoped variable context across nested blocks** — `ruby/parser_test.go:56-106` (constant in class, instance variable in method)
- **Chained call receivers** (`Rails.application.routes.draw`) — `ruby/parser_test.go:104`
- **Array-form callback methods** (`before_action [:authenticate_user!, :set_account]`) — `ruby_dead_code_roots_test.go:173-206`
- **Non-equality script guard (`!=`)** — `ruby_dead_code_roots_test.go:209-246` (only `==` roots the calls)
- **Direct vs transitive lockfile dependency chains** — `ruby/bundler_test.go:63-105`
- **CRLF line endings in lockfile** — `ruby/bundler_test.go:148-161`
- **Nested Bundler group blocks with end balancing** — `ruby/bundler_test.go:168-185`
- **Dependency aliases (`gem "pg", require: "pg")`** — handled by Bundler option parser but not explicitly tested
- **Receiverless call in script guard body** — `ruby_dead_code_roots_test.go:79` (main calls)

## Edge Cases NOT Considered
List edge cases not tested.

- **`class << self` (singleton class) with methods**
- **`def ClassName.method` (non-self singleton method notation)**
- **Redundant `end` in Bundler context stack**
- **Bundler `github:` source in gem call**
- **Bundler `PATH` source section in lockfile**
- **Call node inside nested receiver** (three-level chain)
- **Instance variable read (not assignment)** — `syntax.go:301-306` mentions this but no test.
- **`Operator_assignment` node kind for variables** — `syntax.go:121`
- **Superclass with scope resolution** (e.g., `ApplicationRecord < ActiveRecord::Base`)
- **`respond_to_missing?` as dynamic_dispatch_hook** (only `method_missing` tested)

## Verdict
moderate

The Ruby parser has focused subdirectory tests for core payload extraction, extensive Bundler parsing tests with edge cases (CRLF, nested groups, dependency chains), and parent-level dead-code root tests covering all 5 root kinds plus array-form callbacks and non-equality guards. However, several important AST constructs (singleton class `class << self`, visibility transitions, operator_assignment) lack dedicated tests, and the call deduplication/variable deduplication logic is untested. The Bundler `github:` and `PATH` lockfile sources are also untested.

## Recommended Actions
1. Add a test for `class << self` (singleton class) scope extraction.
2. Add a test for `visibility` transitions (`private`, `protected`) within a class body.
3. Add a test for `rubyNormalizeArgument` covering splat, block, keyword, and quoted args.
4. Add a test for Bundler `github:` source in Gemfile.
5. Add a test for `PATH` section in Gemfile.lock.
6. Add a test for `def ClassName.method` non-self singleton method notation (explicitly documented as out-of-contract but worth verifying).
