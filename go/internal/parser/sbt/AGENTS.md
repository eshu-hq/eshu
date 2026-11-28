# AGENTS.md - internal/parser/sbt guidance

## Read first

1. `README.md` - purpose, row contract, bounds, and exclusions.
2. `doc.go` - godoc contract.
3. `lexer.go` - the single-pass tokenizer (comments, strings, interpolators).
4. `parser.go` - `Parse`, the main dispatch loop, `sbt_state`, val resolution,
   the meta-build flag; `scala_version.go` - the scalaVersion context gate and
   the version envelope.
5. `statement.go` - the statement entry (`parseLibraryDependencies`), the two
   grammar contexts (`keyContext`), the member rule (`settingMemberOf`,
   `applyMember`), the statement end, the fail-closed fallback entry
   (`failStatement`, `fallback`), and superseding (`markReset`);
   `key_watch.go` - the default-deny watch on key tokens a handler consumes
   itself, the legacy axis chain, and the closer read-through;
   `key_list.go` - the key-list settings whose plain key elements are reads.
6. `element.go` - the element and collection grammar, the configuration
   operand, and the infix call allowlist (`classifyCall`).
7. `salvage.go` - the fallback scanner that emits every literal coordinate of
   an unmatched statement as partial; `scope.go` - configuration resolution
   (`resolveConfig`) and the scope table.
8. `rows.go` - Scala binary version and row construction.
9. `../gradle/AGENTS.md` - the sibling manifest parser whose row contract this
   package mirrors.

## Invariants this package enforces

- Dependency direction stays one way: the parent parser package may import
  this package; this package must not import `internal/parser`.
- Never evaluate Scala or run sbt, and never read a sibling file. A form the
  scanner cannot read as a literal is counted in
  `sbt_state.unresolved_declaration_count`, never guessed.
- Every loop consumes at least one token per iteration and nothing recurses,
  so work is linear in the input. A change that adds recursion or lookahead
  beyond one pushed-back token breaks the 200k-parenthesis and 100k-line
  tests in `adversarial_test.go`.
- A configuration string is resolved once (`resolveConfig`) into a small
  `configSpec` and copied by value. Never split, scan, or store a string over
  `MaxConfigBytes`, never re-resolve per dependency, and never build a row
  field from the raw operand. `alloc_test.go` pins this with allocation and
  malloc ceilings; add a case there for any new loop over dependencies.
- A scope the scanner cannot map is `partial` and counted in
  `unresolved_declaration_count`; it is never a resolved `compile`. A row counts
  once, whether partial, unresolved, or both; a statement that emits no row is
  counted once on its own. Add a new
  configuration name to `configPartScope` with a test, or leave it unknown.
- A row is resolved only when its whole statement matches the closed grammar
  in README.md. Never widen it to recover a form: add a production only with
  an accepted-form test, a rejected-form test, and (for a ModuleID call) a
  quote of the method from sbt librarymanagement. Never turn the allowlist into
  a denylist, and never resolve a row from a statement that did not match.
- The fallback is the default. Any token the grammar does not expect sends
  the whole statement to `failStatement`: rows already read become partial and
  the rest of the statement is salvaged as partial rows. Do not add a path that
  skips a token and keeps matching.
- Both keys are default-deny. An occurrence of `libraryDependencies` or
  `scalaVersion` that is no recognised statement and no read (`isKeyRead`
  after `.`, after `.in(...)`, or after the closers of `(k in Test)`) is a
  rewrite: libraryDependencies supersedes every row (`denyLibraryDependencies`),
  scalaVersion poisons the envelope (`unknownScalaVersion`). Never add a
  silent `return` to either handler: every exit is a recognised form, a read,
  or a deny. A key that a handler consumes itself reaches `keyWatch` in
  `scan.next` unclaimed; keep the main loop's `claiming` flag and the
  `unread` restore, or nested keys go unjudged or count twice. The watch
  (`watchStep`) and the handlers (`skipLegacyAxes`, `readThroughClosers`)
  must accept the same read shapes: a shape added to one goes into
  `keyReadExprs` in `read_test.go`, which runs every read through both. Only
  a global reset or a poison sets `scan.cover`; a context reset must not, or
  an inner global rewrite goes uncounted.
- A key named as a value is a read only in `key_list.go`'s shape: an
  allow-listed key-list head (`isKeyListSetting`) in a grammar context, a
  collection literal or a single `+=` key, plain elements, and a list that
  ends the statement. Add a head only with the sbt definition of its type (a
  list of keys). Never accept a key whose element continues, or a list
  followed by anything; and a handler that reads to the list's end must fail
  the list closed (`listBroken`), since its last ')' may not be the list's.
- A member called on the key rewrites when sbt's SettingKey API says it
  returns a Setting that rewrites the key, or returns the key itself
  (`settingMemberOf`); quote the sbt definition for any new name. A member that
  returns no Setting and is no read is denied, so a new read member needs the
  sbt definition and a test before it joins `isKeyRead`. After `withRank(...)`
  the operator decides (`applyRankedOperator`): an appending operator never
  supersedes.
- `scalaVersion` is recorded only in a grammar context. Do not record it from
  a nested position; a wrong literal moves every `%%` hint in the file. An
  unreadable scalaVersion statement in a grammar context, and any
  scalaVersion assignment outside one, poisons the envelope (`scalaPoisoned`)
  for good, and so does every other occurrence but a read (default-deny), and
  two distinct literals report no version: do not add a "prefer
  ThisBuild" or "last wins" pick without a scope-order proof, and do not let
  a nested assignment "change nothing" (a settings val may reach any project).
- `TestGrammarSeededFuzzInvariants` (`fuzz_test.go`) is the regression net:
  the grammar and indentation (`fuzz_indent_test.go`) arms must report zero
  I1, I2, I3, missing and fabricated cases, and the member arm
  (`fuzz_member_test.go`) zero state, extra, count, scala and hint cases. Run it with
  `SBT_GRAMMAR_FUZZ_CASES=30000` after a grammar change. Keep the grammar
  arm's draws unchanged so a seed reproduces the earlier review runs.
- The bare file is the column-0 lines only (`token.lineAtCol0`). sbt 2.x reads
  build.sbt as Scala 3, so an indented line at depth zero is a braceless block:
  never a grammar context, and never a tracked `val`. Do not relax this to
  "depth zero" alone. A closer sets the flag for the rest of its line to the
  flag its opener saw (an opener carries it on the bracket stack), so a closer
  that ends an indented bracket keeps the line indented.
- `dependency_resolution_state` stays in the `resolved`/`partial`/`unresolved`
  vocabulary the reducer and docs depend on.
- `name` is `organization:name` as written. Do not append the Scala suffix to
  it; `sbt_binary_artifact` carries the derived artifact name. The registry
  join reads `sbt_cross_version` and `sbt_binary_artifact`
  (`packageidentity.ManifestConsumptionKeys`): a row without an artifact joins
  nothing, so a change to when `sbt_binary_artifact` is emitted changes which
  Scala dependencies reach a registry package.
- Emitted rows and `sbt_state` are deterministic: rows sort by name with a
  stable sort, and no map iteration order reaches the output.

## Common changes and how to scope them

- Add a failing case to `grammar_test.go` before changing the scanner: an
  accepted-form row for a new production, a rejected-form row for what stays
  partial.
- A new infix call on an element belongs in `classifyCall`, with a positive
  test, an unlisted-name negative test, and its ModuleID definition quoted.
- Plugin coordinates (`addSbtPlugin`) and `project/*.sbt` files other than
  `project/build.sbt` are a separate admission and fact decision, not a change
  here. `project/build.sbt` reaches this parser through the base-name
  registry binding and is flagged `meta_build`.
- Telemetry, registry dispatch, and engine wrappers belong in the parent
  parser package.

## Anti-patterns

- Regex over the whole file. The scanner is token-based so a string or comment
  can never be read as a declaration.
- Treating `val x = "a" % "b" % "1"` outside `libraryDependencies` as a
  dependency.
- Resolving a version through a nested or reassigned val.
