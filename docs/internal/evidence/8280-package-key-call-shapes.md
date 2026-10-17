# Package-Key Member And Function Call Shapes (#8280)

#7601 keyed named JS/TS imports and PHP `new` and static calls on PSR-4
classes. Its arbiter ruling deferred four shapes to land together. This change
keys three of them: JS/TS static and default-import members, PHP instance
methods, and PHP `use function`. The fourth, JS/TS instance method calls on a
`const x = new C()` receiver, is not keyed (see "JS/TS instance calls are not
keyed"). The reducer's unique-or-unresolved rule, the loader SQL, and the
storage read are unchanged: the loader already matches
`package:<package_id>#<export_name>` for any export name, and
`producerstore.PackageName` cuts at the first `#` (`TestPackageName` pins the
new forms).

## Key forms

| Shape | Producer `export_name` | Consumer call |
| --- | --- | --- |
| JS/TS static method | `<Export>.<method>` | `C.m()`, `D.m()` (default import), `ns.C.m()` |
| PHP instance method | `<FQCN>-><method>` | `(new X())->m()`, `$v->m()` with a proven receiver |
| PHP `use function` | `function:<FQN>` | a bare `f()` bound by `use function` |

`<Export>` is the class's own export name, `default` only for the main or
module entry file's default export. Every JS member key holds a `.`, and a
#7601 export name is one identifier or `default`, so `Logger.info()` on a
default import keys `default.info` and can never resolve to a named export
`info`: the false link the #7601 ruling named. PHP static calls keep
`<FQCN>::<method>`, so a static call never matches an instance method. PHP keeps
functions and classes in separate symbol tables, so a function key carries the
`function:` prefix and never collides with a class of the same name.

## Admission rules

- JS/TS producer: a static `method_definition` in the `class_body` of a
  `class_declaration` or `abstract_class_declaration` that the #7601 stamper
  keys (top-level export, `ProducerPackageName`, so the #8244 fixture-directory
  rule applies). The name must be a plain property identifier. Instance
  methods, the constructor, get and set accessors, TS `private` and
  `protected` methods, object-literal methods, and class expressions get no
  key. A method whose name a class field, a TS parameter property, or a
  `this.<name> =` write anywhere in the class body also defines gets no key:
  that property replaces the method. A subclass carries keys only for the
  methods it declares, so an inherited static is a miss.
- JS/TS consumer: the import must be bare, declared, not type-only, and not
  bound twice, and the file must not declare the name again (the #7601 rules,
  unchanged). A member key must also pass a closed import-name rule (review
  F10, ruling (B)): every occurrence of the import's local name in the file
  must be the import itself (or its require declarator), a call, `new`, or JSX
  use of the name or of a member chain on it, a TypeScript type position, or a
  read that runs no user code (the condition of an if, while, do, or ternary,
  the operand of `!` or `typeof`, an operand of `===` or `!==`). Any other
  occurrence of the variable, in any node kind (an import alias, a using
  declaration, a parameter, an assignment, a property write such as
  `Logger.create = f`, a plain read that could hand the class to code that
  replaces a static), disqualifies every member key of that name. Text that
  is never the variable (a property name, a string, a comment, a regular
  expression, a label, JSX text) does not count. A file with a `with`
  statement, a `\u` escape (which can spell the name), or the identifier
  `eval` (a sloppy-mode eval can declare a var) keys no member call.
  `engine_package_member_constructs_test.go` pins the rule over 153 grammar
  constructs. The #7601 keys keep their open rule byte for byte; #8310 tracks
  closing it.
- PHP producer, instance: a public, non-static method with a body of a keyed
  `class_declaration`, not named `__*`. Interface, abstract, trait, and enum
  methods get no key. `final` classes key like any other; an abstract class's
  concrete methods are keyed, though no consumer can `new` it; anonymous
  classes have no name and get no key. A class is keyed only when declared at
  the top level of the file or of a braced namespace body: a class inside a
  condition (`if (!class_exists(...))`) gets no class, static, or instance key.
  This also tightens the #7601 class and static keys. A `class_alias` name has
  no declaration, so a call through the alias is a miss.
- PHP producer, function: a function with no enclosing function, method,
  closure, or type, in a file the nearest named composer.json lists in
  `autoload.files`, where a psr-4 prefix of that manifest covers the FQN
  (`composer.ProducerFunctionPackage`). The vendor and fixture-directory rules
  of `ProducerPackage` apply.
- PHP consumer, instance: the receiver is `(new X(...))`, or a variable that its
  scope (function, method, or closure body, or the script) writes exactly once,
  by `new X(...)`, and otherwise reads only as the object of a member call or
  property access. Passing it as an argument (a by-reference write), `foreach`,
  `list()`, `catch`, `global`, `static`, a closure `use` clause, or a parameter
  of that name rules it out. A scope whose text holds `include`, `require`,
  `$$`, `${`, `extract`, `parse_str`, `eval`, `$GLOBALS`, `goto` (it can skip
  the write), or `ticks` (a tick function runs after any statement) (any case,
  even in a comment) gets no instance keys. A script scope's variables can come from
  the file that includes it and can be rewritten by any code through `global`
  or `$GLOBALS`, even a function in another file (review F2). So in a script
  scope the write must be an earlier statement of a block that holds the call,
  and the code between them may hold only literals and member calls or
  property accesses on the receiver itself: no function, static, or
  constructor call, no read of another variable (a concat may call
  `__toString`), and no `include`, `require`, `eval`, `echo`, `print`, `<?=`,
  inline HTML after `?>` (an output buffer callback may run), `clone`, shell
  command, `global`, or `::` (a class constant or static member may run an
  autoloader) text (review F7 and F9).
- PHP consumer, function: `use function A\f`, `use function A\f as g`,
  `use function A\{f, g}`, and the `function` clause of a mixed group
  (`use A\{B, function f}`). A name bound to two FQNs is blocked. The package is
  the #7601 lock lookup (longest psr-4 prefix, ties, own-namespace guard).

## Proof

Parser tests, on `parser.DefaultEngine`, one file per shape:

- JS/TS static and default members,
  `go/internal/parser/javascript/engine_package_member_keys_test.go`:
  `TestDefaultEngineParsePathTypeScriptStampsProducerStaticMethodKeys`,
  `TestDefaultEngineParsePathTypeScriptSkipsProducerMethodKeysUnderFixtureManifest`,
  `TestDefaultEngineParsePathTypeScriptStampsConsumerStaticMemberKeys`,
  `TestDefaultEngineParsePathTypeScriptSkipsShadowedMethodKeys`,
  `TestDefaultEngineParsePathTypeScriptSkipsStaticMembersOfShadowedImports`,
  `TestDefaultEngineParsePathSkipsMemberKeysTheOpenRuleMisses`,
  `TestDefaultEngineParsePathSkipsMemberKeysInFilesWithWith`, and
  `TestDefaultEngineParsePathKeepsTheOpenRuleForPlainCalls`, plus the
  construct table `TestDefaultEngineParsePathKeysStaticMembersOnlyUnderTheClosedRule`
  in `go/internal/parser/javascript/engine_package_member_constructs_test.go`.
- PHP instance methods, `go/internal/parser/php/package_keys_instances_test.go`:
  `TestDefaultEngineParsePathPHPStampsProducerInstanceMethodKeys`,
  `TestDefaultEngineParsePathPHPStampsConsumerInstanceKeys`,
  `TestDefaultEngineParsePathPHPSkipsUnprovableInstanceKeys`,
  `TestDefaultEngineParsePathPHPKeysTopLevelScriptInstanceCalls`,
  `TestDefaultEngineParsePathPHPSkipsConditionalAndAnonymousClassKeys`, and
  `TestDefaultEngineParsePathPHPSkipsReceiversNonLocalCodeCanRewrite`.
- PHP `use function`, `go/internal/parser/php/package_keys_functions_test.go`:
  `TestDefaultEngineParsePathPHPStampsProducerFunctionKeys`,
  `TestDefaultEngineParsePathPHPSkipsProducerFunctionKeysUnderFixtureManifest`,
  `TestDefaultEngineParsePathPHPStampsConsumerFunctionKeys`, and
  `TestDefaultEngineParsePathPHPSkipsUnprovableFunctionKeys`.

RED: every positive row failed with an empty key before its shape existed
(for example `package_id missing, want "acme/logging"` on `Logger->info`).
Each review counterexample of four rounds went RED first on the real parser.
For the closed import-name rule, the new tests run against the earlier branch
head without it keyed `Logger.create()` in all four #8310 repros and in every
plain-read and write row of the construct table. Mutations that removed one
guard each failed a named row: the closed rule (78 rows), the with, `\u`, and
eval file guards, the type-position allowance, the shadowing rule, and, for
PHP, the single-write rule, the scope blockers (`extract`, `eval`, `$GLOBALS`,
`goto`, `ticks`), the script-region rule (other variables, calls, inline HTML,
`::`), the dominance check, the closure `use` capture, the magic-name, body,
nested-function, `autoload.files`, and psr-4 checks.

Four #7601 rows changed on purpose: `render.member()` now keys
`#default.member`, `fmt.nested.deep()` keys `#nested.deep`, a public PHP
instance method keys `->`, and `helper()` under `use function` keys
`function:`. The #7601 JS plain-call keys are otherwise byte-identical to
main: `redeclaredImportNames` and `isBindingIdentifier` are unchanged (review
F11 removed a TS `module` kind an earlier fix had added), and over the
49-repository QA sample (below) the base and head parsers emit the same
21,548 #7601-form JS/TS keys, compared line by line.
`TestDefaultEngineParsePathKeepsTheOpenRuleForPlainCalls` pins a `module fmt {}`
rebinding: both `fmt.f()` calls keep their #7601 key as on main (the inner one
is the #8310 hole), while the file's new member key stays unkeyed.

Correlation truth matrix (reducer, parser-backed, one file per shape under
`go/internal/reducer/code/call/`: `cross_repo_static_member_keys_test.go`,
`cross_repo_php_instance_keys_test.go`, `cross_repo_php_function_keys_test.go`):

| Axis | Proof |
| --- | --- |
| Positive | `ReferencedSymbolKeys` holds each new key, and `ExtractRows` writes `CALLS` rows with `resolution_method=import_binding` to the producer's `create` and `connect` (JS) and `info` and `format_message` (PHP) |
| Negative | A non-exported class's static method gets no row |
| Ambiguous | Two repositories publishing the package give no row for any new key |

With the parser reverted in a throwaway worktree, every positive test failed.

Loader proof on real Postgres: one `TestReducerContentionGateActiveCodeCallSymbolLoaderResolves*`
test per shape in
`go/internal/storage/postgres/active_symbol_loader_member_keys_live_test.go`
parses producers and consumers with `parser.DefaultEngine`, stores the
payloads as active file facts with their manifests, and calls the unchanged
`LoadActiveCodeCallSymbolDefinitionFacts` with each key the consumer carries.
Each key form loads exactly its producer fact, and a mirror repository of the
same npm package returns both candidates. It passes on a local PostgreSQL
18.6 and is enrolled in the live-postgres-readiness runner.

Other readers of the key: `codeCallDefinitionSymbolKeys` and
`codeCallEdgeSymbolKeys` (`symbol_index.go`) concatenate the fields;
`producerstore.PackageName` cuts at the first `#` (`TestPackageName`); the
#7610 barrier's `nodePackageKeyPackage` cuts at the last `#`
(`TestNodePackageKeyPackage` holds member keys). Projection copies
`package_id` and `export_name` into entity metadata as opaque strings, and no
query, MCP, or search surface reads them. No export name can hold `#`: a JS
member name must be a plain identifier (a `#private` name is excluded), a PHP
name passes `phpIsPlainName`, and composer names follow the composer grammar.

## JS/TS instance calls are not keyed

Review rounds 1 to 3 hardened a JS/TS instance receiver proof (`x.m()` where
the file binds x once, by `const x = new C()`): scope checks (F1), a linear
scan (F4), TypeScript class bindings (F6), and finally a closed occurrence
rule (F10). Under the closed rule the QA sample kept 116 keyed instance calls
and 0 resolved (17 before it): every consumer file that resolved also assigned
the receiver in another scope or passed it to a function, which a parser cannot
prove safe. With no resolved edge to hand-check, the shape was dropped from
this change (lead ruling). A follow-up needs a proof a parser can give, such as
type information.

## Content version

`gitGenerationContentVersion` moves from `git-collector.v6` to
`git-collector.v7`. The `content_version` fixture gains a JS static method and
a declared-dependency consumer (`js-app`), and a PHP instance method, an
`autoload.files` function, and a locked consumer (`php-app`). The pin moved
from 266 to 289 facts (digest `a8423e39...`). With only the parser directories
reverted to the base commit in a throwaway worktree,
`TestGenerationContentVersionPinsCollectorOutput` failed (digest
`5a858ebb...` against pinned `a8423e39...`), so a parser-only revert cannot pass the pin.

## QA measurement

Read-only, on the QA environment, 2026-10-17. The new keys exist only after a
reindex, so the deployed graph cannot show new edges yet. Instead, the source
of 49 repositories was read from the replica and parsed locally with the final
branch parser: the repositories of the 40 largest cross-repository
`import_binding` repository pairs, plus every repository with a `use function`
import or an `autoload.files` manifest. Keys were joined the way the loader and
reducer do: a key resolves only when exactly one definition carries it. The
source copies were deleted after each run.

The "Keyed calls" column is the per-shape recall measurement: the call sites in
this sample that each key form covers. "Resolved" is the subset whose producer
is also in the sample.

| Shape | Keyed calls | Resolved | Ambiguous | No definition |
| --- | --- | --- | --- | --- |
| JS/TS static (named and namespace) | 4,077 | 15 | 0 | 4,062 |
| JS/TS default-import static | 1,354 | 0 | 0 | 1,354 |
| PHP instance | 190 | 179 | 0 | 11 |
| PHP `use function` | 0 | 0 | 0 | 0 |

Every resolved call is cross-repository. The 15 JS/TS resolutions passed a
structural check (the import names the producer's package, and the producer
line declares the static method on that exported class); four used a
multi-line import and were read by hand. All 179 PHP resolutions (14 consumer
and producer pairs, 72 methods) passed the same kind of check: the class is
bound by `use` or a fully qualified `new`, the call is on the line, and the
producer line declares a public, non-static method of that class. Across the
runs, 6 JS/TS static and 18 PHP calls were read in full: 24 true, 0 false. The
closed import-name rule removed 151 JS/TS static and 231 default-import keys
and no resolution. The PHP review fixes removed 153 PHP keys and 143
resolutions, mostly script-scope template calls with code between the write
and the call.

The corpus has 579 `use function` imports in 439 PHP files across 4
repositories. Three of the four store no `composer.lock`, and the fourth's
2 files bind namespaces no lock covers, so no call is keyed. The PHP function
shape ships on fixture proof alone.

## Performance

Performance Evidence: interleaved base `6b0340cf7` and head test binaries, 8
rounds each, `-cpu 1` (JS `-benchtime 10x`, PHP `100x`), on a shared 16-core
host under load (load average 15 to 21), 2026-10-17.

| Benchmark | Base median (min to max) | Head median (min to max) | Delta | Allocs/op |
| --- | --- | --- | --- | --- |
| `BenchmarkParsePathTypeScriptPackageImportCalls` (#7601 worst case) | 394.41 ms (393.25 to 403.54) | 397.10 ms (392.58 to 416.11) | +0.7% | 992,274 to 992,475 |
| `BenchmarkParsePathTypeScriptPackageMemberCalls` (new, 200 static calls and the closed-rule scan) | 97.79 ms (96.89 to 104.18) | 103.15 ms (100.68 to 107.18) | +5.5% | 270,747 to 280,689 |
| `BenchmarkParsePathPHPRouteHeavy` (no composer files) | 31.08 ms (30.70 to 32.39) | 31.81 ms (30.98 to 32.97) | +2.3% | 84,111 to 84,311 |
| `BenchmarkParsePathPHPWithComposerLock` (#7601 gate) | 10.40 ms (10.29 to 10.48) | 10.55 ms (10.29 to 10.90) | +1.4% | 31,165 to 31,213 |
| `BenchmarkParsePathPHPMemberCalls` (new, 40 receiver scopes under a 120-package lock) | 24.54 ms (24.22 to 25.36) | 25.79 ms (25.28 to 26.65) | +5.1% | 69,516 to 75,027 |

The closed import-name rule scans the source once per file that holds a
member key; files without one pay nothing. The PHP receiver proof classifies
only candidate variables from the `variable_name` nodes phase 1 already
gathered; a first version that walked each scope subtree cost +14.6% on
`BenchmarkParsePathPHPMemberCalls` (base `db97990fe`), and a CPU profile put
12% of the parse in that walk.

Observability Evidence: no new signal. Resolved edges carry
`resolution_method=import_binding`, and the existing `code call symbol
definition load` log counts package keys and producer scopes.

## Known limits

- JS/TS instance method calls are not keyed (see above).
- #8310: the #7601 JS/TS plain-call keys (`local()`, `new Local()`, `<Local />`,
  `ns.member()`) still use the open redeclaration list on main. An import
  alias, a using declaration, a `\u` escape, or a sloppy-mode eval var that
  rebinds the import name, and a `with` statement, can leave such a call keyed
  to the package. The #8280 member keys are closed against all of these.
- Producer side, JS/TS: a static method that its own file reassigns through
  the class name (`Class.m = ...`, `Class["m"] = ...`, `Object.assign(Class,
  ...)`, `Object.defineProperty(Class, ...)`, `delete Class.m`, a destructuring
  write) or that another file rewrites is still keyed. The edge then points at
  the original definition, which a wrapper usually calls. A field, a parameter
  property, or a `this.<name> =` write in the class body does drop the key.
  Tracked with the #8310 follow-up list.
- Recall: a typed parameter or property (`Logger $l`, `$this->logger->m()`), an
  interface-typed receiver, an inherited or trait method, and a re-exported
  class are misses, never links. A JS/TS import name used anywhere outside the
  closed rule's positions keys no member call of that name.
- A PHP function reached only through a file another listed file includes
  (`functions_include.php` requiring `functions.php`) is a miss.
- Script-scope PHP receivers: the rule assumes that the receiver's own methods
  and magic methods (the producer class's code) do not rewrite the script
  variable that holds the receiver. Every other path the review rounds found
  blocks the key: `$GLOBALS`, `eval`, `global` in a called function, any code
  between the write and the call, a `goto` that skips the write,
  `declare(ticks)`, inline HTML (output buffer callbacks), and `::`
  (autoloaders).
- PHP method names are case-insensitive; the match is case-sensitive, so a case
  mismatch is a miss.

## NOT_CHECKED

- Deployed false-link check (5 new edges per language on the QA graph, 0 false):
  NOT_CHECKED until the image is deployed and the QA corpus is reindexed.
- PHP `use function` on real data: no QA call is keyed (see above).
- B-7 golden corpus run with the new keys: not run.
