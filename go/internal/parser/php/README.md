# PHP Parser

## Purpose

This package owns the tree-sitter PHP parser adapter used by the parent parser
engine. It extracts namespace metadata, imports, classes, interfaces, traits,
functions, variables, call rows, receiver inference, anonymous classes, and
trait-use adaptation evidence by walking the tree-sitter AST.

## Ownership boundary

The package is responsible for PHP source parsing and payload bucket
population. The parent parser package still owns registry dispatch, engine
orchestration, repo path handling, tree-sitter runtime/grammar caching, and
parse telemetry. `Engine.parsePHP` obtains a `php` parser from the shared
runtime and passes it to `Parse`.
The parser also emits exact `framework_semantics.symfony.route_entries` for
method-level attributes resolved to Symfony `Route` whose path and HTTP methods
are literal, and bounded `dead_code_root_kinds` for PHP entrypoints,
constructors, known magic methods, same-file interface and trait methods,
route-backed controller actions, ZF1 convention-dispatched actions, literal
route handlers, Symfony route attributes, and WordPress hook callbacks.
Functions also carry `framework` when exactly one framework's evidence
touches them (a Symfony Route attribute, a WordPress hook callback name, or
a ZF1 controller action); ambiguous functions stay untagged so the dead-code
no-root-model notice never fires on a misattribution (#7712).

The engine-level PHP regressions live in this directory as the `php_*_test.go`
family (18 files in the external `php_test` package: 16 test files, the
`php_parse_bench_test.go` benchmark, and `php_test_helpers_test.go`), carrying
55 test functions and `BenchmarkParsePathPHPRouteHeavy`. They were relocated
from `go/internal/parser` by #6062, following the Elixir precedent (#6335), and
still drive `parser.DefaultEngine().ParsePath` from outside the implementation
so they prove the exported contract rather than internals. Coverage spans
declarations, imports, grouped `use`, magic-method classification, variable and
call metadata, every receiver-inference family (typed `$this` properties, typed
parameters, `new`, aliases, method and free-function return chains,
self/static/parent scopes, static properties, namespaced and parenthesized
receivers), trait adaptations, anonymous classes, Symfony/Slim/Laravel route
entries, and the dead-code root kinds. Five in-package `package php` test files
(9 test functions) cover walk counts, parent-lookup cgo crossings, the
`PHP_PARSE_DUMP` differential, and array-literal route targets.
`php_test_helpers_test.go` holds only what `internal/parser/parsertest` lacks:
the fixture-path resolver, a `*testing.B` file writer, string-contains, bool,
`[]any`, nil-field, and call-context-tuple assertions. The by-field row lookup
is not local: it is `parsertest.AssertBucketItemByFieldValue`, which this
package's tests use in 37 places.

## Exported surface

The godoc contract is in doc.go. Current exports are `Parse` and `PreScan`,
both of which take a `*tree_sitter.Parser` configured for the PHP grammar.
`Parse` also takes the repository root, which bounds the composer reads behind
the cross-repository package keys below; an empty root stamps no keys.

## Dependencies

This package imports the Go standard library, `internal/parser/shared`,
`internal/parser/php/composer` (composer.json and composer.lock reads), and
`github.com/tree-sitter/go-tree-sitter`. The grammar is
`github.com/tree-sitter/tree-sitter-php` (LanguagePHP), wired into the parent
runtime loader. Production code and the in-package tests must not import the
parent `internal/parser` package. The external `php_test` files are the one
exception: they import the parent parser only to reach `DefaultEngine` and
`Options`, and `internal/parser/parsertest` for the bucket and field assertions
shared with the other language packages.

## Cross-repository package keys (#7601)

`package_keys.go` writes both halves of the `package:<package_id>#<export_name>`
key the reducer already resolves:

- Producer: a class or interface gets `package_id` (the nearest
  `composer.json` name in the composer grammar) and `export_name` (its fully
  qualified name) only when an `autoload.psr-4` prefix of that manifest maps the
  class to exactly its file. A public static class method gets
  `export_name = <FQCN>::<method>`. No key under `vendor/`, for `autoload-dev`,
  `classmap`, `psr-0`, the empty fallback prefix, traits, enums (not emitted),
  free functions, or in a file with more than one namespace declaration.
- Consumer: `new X(` and `X::method(` get `package_export_symbol` when `X` is
  bound by a `use` import (alias and group use included) or written as a fully
  qualified literal, and a `composer.lock` between the file and the repository
  root names the providing package through the longest covering psr-4 prefix.
  An own autoload prefix of the consumer at least as long as that locked
  prefix suppresses the key, and a `function` or `const` clause in a mixed group
  use binds no class.
  Not keyed: `self`/`static`/`parent`, `$var::`, `new $var`, relative qualified
  names, `use const`, type references, `X::class`, and constants.
- #8280 (`package_keys_instances.go`, `package_keys_scopes.go`, `package_keys_functions.go`): a public, concrete, non-magic instance
  method gets `export_name = <FQCN>-><method>`, keyed on `(new X())->m(` and on
  `$v->m(` when the scope writes `$v` once, by `new X(`, and reads it only as a
  member receiver (a script scope also needs the write to run first on every
  path, with only literals and calls on the receiver between them; `eval` or
  `$GLOBALS` anywhere in the scope blocks it). A namespaced free function in a file the manifest's `autoload.files`
  lists, under a covering psr-4 prefix, gets `export_name = function:<FQN>`,
  keyed on a bare call bound by `use function`. Typed parameters, properties,
  interface, trait, and inherited methods stay unkeyed, and a class declared
  inside a condition (`if (!class_exists(...))`) gets no key of any kind.

See `composer/README.md`, `docs/internal/evidence/7601-php-package-keys.md`, and
`docs/internal/evidence/8280-package-key-call-shapes.md`.

## Telemetry

This package emits no telemetry. Parse timing remains owned by the parent
parser engine.

## Gotchas / invariants

PHP parsing is AST-driven. Declarations resolve `name`, `base_clause`,
`class_interface_clause`, and trait `use_declaration` nodes; properties and
parameters resolve declared type nodes (`named_type`, `optional_type`,
`union_type`, `primitive_type`); calls resolve `member_call_expression`,
`nullsafe_member_call_expression`, `scoped_call_expression`,
`object_creation_expression`, and `function_call_expression`. Receiver chains
are reconstructed from receiver node source text, with nullsafe `?->`
normalized to `->` and the final `->method` rendered as `.method` in
`full_name`. Call rows are deduplicated by full name and source line so repeated
calls on different lines remain visible. PreScan uses a declaration-only AST
walk over functions, methods, classes, traits, interfaces, and anonymous
classes, then sorts the names so repository import-map discovery does not pay
for full variable/call/dead-code semantic extraction before the full parse
stage. Type inference resolves receivers through a two-pass walk so
declarations later in
the file still inform earlier call sites. Symfony route entries stay bounded to
source-proven method attributes whose imported, aliased, or fully qualified name
resolves to Symfony `Route` with literal path and method arguments; dynamic
attributes, Composer/autoload breadth, reflection, and dynamic dispatch remain
query-layer exactness blockers. Route-attribute candidates are recorded during
phase 1 (not walked separately) and resolved to route entries after phase 1
finishes, once every import in the file is known.

Performance Evidence: `go test ./internal/parser -run '^$' -bench
BenchmarkPreScanSelectedLanguages -benchmem -benchtime=1x -count=1` measured
the PHP 10K LOC pre-scan fixture at `194719834 ns/op`, `36197800 B/op`, and
`1180568 allocs/op` before the declaration-only pre-scan, versus
`56014375 ns/op`, `4063064 B/op`, and `134765 allocs/op` after.
Compatibility is guarded by `TestPreScanMatchesParseDeclarationNames`.

Performance Evidence: Parse used to run four independent full-tree AST
traversals per file: `buildPHPParentLookup` (parent-edge index, a separate
stack-based walk over named and unnamed children), `collectPHPDeclarations`
(phase 1: declarations/imports/type evidence/dead-code facts, a
`shared.WalkNamed` pass), `emitPHPVariablesAndCalls` (phase 2:
variables/calls, a second `shared.WalkNamed` pass), and a dedicated
route-attribute `shared.WalkNamed` walk inside `buildPHPFrameworkSemantics` for
`framework_semantics.symfony.route_entries`. The route walk visited `attribute`
nodes that phase 1 already visits for `observePHPAttribute`, so it now folds
into phase 1: `collectPHPDeclarations` records candidate route attribute nodes
in source order, and `buildPHPFrameworkSemantics`/`phpSymfonyRoutes` resolve
those candidates against the fully-collected import set after phase 1
completes, with no traversal of their own. This drops the `shared.WalkNamed`
call count from 3 to 2 per file (the parent-lookup walk is unaffected, since it
is not `shared.WalkNamed`); phase 1 and phase 2 stay separate two-pass walks
because phase 2 depends on whole-file type evidence (property types,
method/function return types, import aliases) that phase 1 only finishes
collecting once it has seen the entire file — see "Do not collapse the two
passes" in AGENTS.md. `TestParseFullTreeWalkCount` in this package installs
`shared.SetWalkNamedHookForTest` and pins the count at exactly 2 `WalkNamed`
calls per `Parse`, so it fails if a future change restores the route walk (or
any other pass) as a `shared.WalkNamed` call, regardless of where in the
package that call is added. `go test ./internal/parser/php -run '^$' -bench
BenchmarkParsePathPHPRouteHeavy -benchmem -count=10` (the benchmark lives in
this package's `php_parse_bench_test.go` since #6062) plus `benchstat` on a
synthetic 60-method, Symfony-Route-attributed PHP controller measured
`14.53ms ± 6%` (benchstat's `14.53m` in `sec/op` units) before this change
versus `12.58ms ± 5%` (`12.58m`) after (`-13.44%`, `p=0.000`, `n=10`);
allocations fell from `89.16k` to `81.23k` per op (`-8.89%`) and bytes/op from
`2.819Mi` to `2.624Mi` (`-6.90%`). Emitted payload shape stayed byte-identical
across all 18 tracked PHP fixtures under `tests/fixtures/`, confirmed by
diffing `ParsePath` output before and after with worktree-path fields
stripped.

Performance Evidence: remote collector-discovered five-repo parse profiling
on 2026-07-02, with 16 parse workers and NornicDB PR #230 backend bits,
measured PHP parser parent lookup before/after on the same corpus slice. The
sum of per-file parse durations across the five profiled repositories fell from
`65570.959 ms` to `48307.175 ms`. The three largest PHP-heavy repositories in
that slice improved from `24870.458 ms` to `11293.407 ms` (-54.6%),
`6288.188 ms` to `3436.674 ms` (-45.3%), and `4583.049 ms` to `3685.342 ms`
(-19.6%).
`TestPHPParentLookupEliminatesScopeContextParentCgoCrossings` proves the
production scope/context helpers preserve `Node.Parent()` identity while
reducing cgo crossings on the regression fixture from `31512` to `16726`.

No-Observability-Change: this parser package emits no metrics, spans, or logs.
Operators continue to diagnose parser cost through collector snapshot stage
logs and `eshu_dp_file_parse_duration_seconds`.

## Related docs

- docs/public/languages/support-maturity.md
