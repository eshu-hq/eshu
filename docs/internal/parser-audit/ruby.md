# Ruby Parser Audit

## Overview
The Ruby parser (`go/internal/parser/ruby/`) is a tree-sitter-backed adapter that extracts modules, classes, singleton classes, methods, imports (`require`/`require_relative`/`load`), module inclusions (`include`), variables (constants, identifiers, instance variables), method calls (dotted and receiverless), block end lines, Rails-idiomatic dead-code root metadata, and Bundler dependency evidence from `Gemfile`/`Gemfile.lock`. The AST walk in `syntax.go` uses a scope stack for context resolution. Call extraction uses `calls.go` for dotted name composition. The package holds 4 external `package ruby_test` files (and one helper file) that drive the public `parser.DefaultEngine().ParsePath` contract from inside the package directory, alongside its existing same-package tests. The parent `go/internal/parser` directory keeps only cross-language Ruby coverage: the long-tail fixture, cyclomatic complexity, dependency-coverage, registry, runtime, and benchmark suites.

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
10. **Dead-code root kinds** (`dead_code_roots.go:42-62`, `:82-99`):
    - `ruby.rails_controller_action` — emission `dead_code_roots.go:54-60`, gated by the same-file transitive superclass-chain walk `rubyIsRailsController` (`:328-360`, accepted bases `:285-291`, action-name filter `rubyIsRailsControllerActionName` `:362-369`)
    - `ruby.rails_callback_method` (`dead_code_roots.go:84-88`)
    - `ruby.dynamic_dispatch_hook` (`dead_code_roots.go:51-53`)
    - `ruby.method_reference_target` (`dead_code_roots.go:89-93`)
    - `ruby.script_entrypoint` (`dead_code_roots.go:94-98`)
    - Rails callback methods: `before_action`, `after_action`, `around_action`, `before_filter`, `after_filter`, `around_filter` (`dead_code_roots.go:22-29`)
    - Reflection methods: `method`, `send`, `public_send` (`dead_code_roots.go:33-37`)
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
11. **Rails controller action root** — `ruby/ruby_dead_code_roots_test.go:78` (`TestDefaultEngineParsePathRubyEmitsDeadCodeRootKinds`)
12. **Rails callback method root** — `ruby/ruby_dead_code_roots_test.go:79`
13. **Dynamic dispatch hook root** — `ruby/ruby_dead_code_roots_test.go:80-81`
14. **Script entrypoint root** — `ruby/ruby_dead_code_roots_test.go:82`
15. **Method reference target root** — `ruby/ruby_dead_code_roots_test.go:83`
16. **Dead code fixture expected roots** — `ruby/ruby_dead_code_roots_test.go:89-111` (comprehensive fixture)
17. **Receiverless helper calls** — `ruby/ruby_dead_code_roots_test.go:115-171` (`TestDefaultEngineParsePathRubyEmitsReceiverlessHelperCalls`)
18. **Array callback methods** — `ruby/ruby_dead_code_roots_test.go:176-209` (`before_action [:a, :b]`)
19. **Non-equality script guard rejected** — `ruby/ruby_dead_code_roots_test.go:358-395` (`TestDefaultEngineParsePathRubyRejectsNonEqualityScriptGuard`)
20. **Bundler Gemfile dependencies** — `ruby/bundler_test.go:13-59` (direct deps, groups, sources)
21. **Bundler lockfile dependencies** — `ruby/bundler_test.go:63-105` (exact versions, dependency paths)
22. **Git source in lockfile** — `ruby/bundler_test.go:111-145`
23. **CRLF line endings** — `ruby/bundler_test.go:148-161`
24. **Nested group/block end balancing** — `ruby/bundler_test.go:168-185`
25. **Cyclomatic complexity** — `engine_cyclomatic_complexity_test.go:167-181` (2 test cases)
26. **Long-tail comprehensive fixture** — `engine_long_tail_test.go:12-15` (`TestDefaultEngineParsePathRubyFixtures`)
27. **`method_missing` / `respond_to_missing?` as dynamic dispatch** — `ruby/engine_ruby_semantics_test.go:220` (`TestDefaultEngineParsePathRubyDistinguishesSingletonAndDynamicDispatchMethods` defines both methods and asserts `type=dynamic_dispatch` for each at :271-272); root metadata separately at `ruby/ruby_dead_code_roots_test.go:81` (`ruby.dynamic_dispatch_hook`).

## Unverified / Claimed-but-Untested Constructs
List constructs claimed but not covered by any test.

1. **Singleton class (`class << self`)** — `syntax.go:184-199`: not tested in any test file. Neither the singleton class scope nor methods inside it are explicitly verified.
2. **`def self.name` singleton method** (`syntax.go:211`): tested indirectly via `OrdersController.self.call`, but not isolated.
3. **Visibility transitions** (`public`/`private`/`protected` toggles) — `syntax.go:228-229`: the public/private interaction with Rails controller action marking is tested, but visibility transitions within a class body are not.
4. **Assignment-side bare identifier calls** (`calls.go:44-59`): the `x = build_scopes` pattern is not tested in isolation; only dotted calls from the main test cover call extraction.
5. **Call deduplication by full name + line** (`calls.go:64-66`): not explicitly tested with duplicate calls on the same line.
6. **Variable deduplication across scopes** — `syntax.go:318-321` (`seenVariables`): not tested with a variable assigned in two scopes.
7. **`rubyNormalizeArgument` edge cases** — `calls.go:185-207`: not tested with splat, block, keyword, or quoted arguments.
8. **`rubyInferAssignmentType`** — `calls.go:167-181`: not tested with `new ` prefix stripping or terminal handling.
9. **Opaque block balancing in Bundler** (`bundler_blocks.go`): tested implicitly through group tests, but not in isolation.
10. **Bundler `github:` source type** (`bundler_gemfile.go:15,69,91`): no test with `github "user/repo" do`.
11. **Bundler `source` option within group context** (`bundler_gemfile.go:99-103`): not tested.

## Edge Cases Considered
List edge cases the tests actually cover with test references.

- **Scoped variable context across nested blocks** — `ruby/parser_test.go:56-106` (constant in class, instance variable in method)
- **Chained call receivers** (`Rails.application.routes.draw`) — `ruby/parser_test.go:104`
- **Array-form callback methods** (`before_action [:authenticate_user!, :set_account]`) — `ruby/ruby_dead_code_roots_test.go:176-209`
- **Non-equality script guard (`!=`)** — `ruby/ruby_dead_code_roots_test.go:358-395` (only `==` roots the calls)
- **Direct vs transitive lockfile dependency chains** — `ruby/bundler_test.go:63-105`
- **Bundler lockfile `GIT` section** — `ruby/engine_bundler_lockfile_test.go` (asserts `source_type=git`, `source_path=https://github.com/rails/rails.git`). This is the lockfile path, not the Gemfile gem-call `github:` option at `bundler_gemfile.go:15,69,91`, which remains untested — see Unverified item 10 and Recommended Action 4.
- **Bundler `PATH` local source** — first covered at `ruby/bundler_test.go:108-145` (`TestParseGemfileLockPreservesGitAndPathAmbiguity`, `source_type=path`, `source_path=../components/local`); `ruby/engine_bundler_lockfile_test.go` adds the same assertion through the parent Engine (`source_path=../vendor/gems/auth`).
- **CRLF line endings in lockfile** — `ruby/bundler_test.go:148-161`
- **Nested Bundler group blocks with end balancing** — `ruby/bundler_test.go:168-185`
- **Dependency aliases (`gem "pg", require: "pg")`** — handled by Bundler option parser but not explicitly tested
- **Receiverless call in script guard body** — `ruby/ruby_dead_code_roots_test.go:82` (main calls)

## Edge Cases NOT Considered
List edge cases not tested.

- **`class << self` (singleton class) with methods**
- **`def ClassName.method` (non-self singleton method notation)**
- **Redundant `end` in Bundler context stack**
- **Call node inside nested receiver** (three-level chain)
- **Instance variable read (not assignment)** — `syntax.go:301-306` mentions this but no test.
- **`Operator_assignment` node kind for variables** — `syntax.go:121`
- **Superclass with scope resolution** (e.g., `ApplicationRecord < ActiveRecord::Base`)

## Verdict
moderate

The Ruby parser has focused subdirectory tests for core payload extraction, extensive Bundler parsing tests with edge cases (CRLF, nested groups, dependency chains), and package-local external Engine tests covering all 5 dead-code root kinds plus array-form callbacks and non-equality guards. However, several important AST constructs (singleton class `class << self`, visibility transitions, operator_assignment) lack dedicated tests, and the call deduplication/variable deduplication logic is untested.

## Recommended Actions
1. Add a test for `class << self` (singleton class) scope extraction.
2. Add a test for `visibility` transitions (`private`, `protected`) within a class body.
3. Add a test for `rubyNormalizeArgument` covering splat, block, keyword, and quoted args.
4. Add a test for Bundler `github:` source in Gemfile.
5. Add a test for `def ClassName.method` non-self singleton method notation (explicitly documented as out-of-contract but worth verifying).
