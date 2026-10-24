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
   itself, the legacy axis chain, and the closer read-through.
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
  `unresolved_declaration_count`; it is never a resolved `compile`. Add a new
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
  it; `sbt_binary_artifact` carries the derived artifact name.
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

## Evidence notes

### build.sbt scanner cost (issue #8938)

Performance Evidence: baseline is the sibling Gradle manifest parser, measured
with `go test -run '^$' -bench 'BenchmarkParseConfig/(sbt|gradle)$' -benchmem
-count=3 ./internal/parser` on the same host and the same engine path
(`Engine.ParsePath`, linux/arm64, Go 1.26, shared host at load average 4 to
9). Input shape: a synthetic manifest of about 10,000 lines (10,001 for Gradle,
10,000 for sbt), one literal dependency per line. With the closed grammar sbt
takes 15.2 to 15.4 ms/op (49.4 to 50.0 MB/s), 16.4 MB/op, 139,757 allocs/op;
Gradle takes 54.9 to 56.0 ms/op (11.3 to 11.5 MB/s), 12.7 to 12.9 MB/op,
119,732 to 119,774 allocs/op. The allowlist-tail scanner it replaces measured
13.5 to 13.6 ms/op and 16.4 MB/op in the same session, so the statement checks
cost about 13 percent of parse time and no bytes; sbt stays about 3.6 times
faster than Gradle and allocates about 29 percent more bytes, because it builds
one row map per dependency. The column-0 rule for Scala 3 braceless blocks
(one flag per token, set once per line) re-measured at 15.3 to 15.5 ms/op,
16.4 MB/op and 139,757 allocs/op on the same benchmark at load average 1.7:
no byte or allocation change. A 120-byte `dependency` struct measured 18.1 MB/op
(slice growth into larger size classes), which is why `ctx` and `line` are
int32. This is a new parser on a path no existing file reaches (`build.sbt`
was unadmitted), so existing files see no change.

No-Regression Evidence (configuration bound): before the bound, `alloc_test.go`
failed with `.map(_ % "<100 KB>")` over 1,000 dependencies at 3,934,180,448
bytes allocated (32,804 times the 119,930-byte input), a 10 MB `"a,a,..."`
configuration at 1,035,783,832 bytes (103.6 times), and 20,000 chained
`.map(_ % Test)` over 5,000 dependencies at 200,070,096 mallocs. With the
closed grammar the same tests allocate about 1.42 million bytes (11.8 to 11.9
times across runs; 1,000 rows dominate), about 20.0 million bytes (2.0 times)
and about 55,100 mallocs. 200,000 repeated
`% "test"` operands and 100,000-element tails (allowlisted calls, unknown
calls, a `.map` body, leading-infix lines) allocate 2.0 times the input: the
read buffer plus its string copy. Counts of bytes and objects, not time.

No-Regression Evidence (grammar): the seeded fuzz in `fuzz_test.go` reported
I1=1,218, I2=155 and I3=1,208 over 4,000 cases on the allowlist-tail scanner,
and I1=0, I2=0, I3=0, missing=0, fabricated=0 with the closed grammar, at
4,000 cases and at 30,000 (140,469 rows).

No-Regression Evidence (indentation): the indentation arm wraps generated
statements in Scala 3 braceless blocks and declares block-local vals that
shadow top-level ones. On the scanner before the column-0 rule it reported
I1=60, I2=15, I3=83 over 4,000 cases and I1=441, I2=106, I3=606 over 30,000;
with the rule, zero at both (18,779 and 140,514 rows), and the grammar arm
stays zero. Those counts predate the closer shapes. The arm now also places a
statement or a block-local val after `}; `, `); ` or `]; ` that closes a
bracket opened on an indented line; with the column-0 flag taken from the
line's first token alone it reported I1=14, I2=3, I3=19 over 4,000 cases and
I1=107, I2=7, I3=149 over 30,000, and with the opener's flag carried on the
bracket stack, zero at both (same row counts). The closer flag re-measured at
15.3 to 15.4 ms/op, 16.4 MB/op and 139,757 to 139,758 allocs/op at load
average 0.6 to 0.9.

No-Regression Evidence (member rule, operators, scalaVersion context): the
member arm interleaves resolved column-0 statements with member calls on the
key, `<<=`, `<+=`, `<++=` and unknown assignment operators, and top-level and
nested scalaVersion literals, and checks exact row states, the exact
`unresolved_declaration_count`, `scala_version`, `scala_version_count` and the
`%%` hint. On the scanner before the rule it reported state=5,378,
count=2,752, scala=1,494 and hint=773 over 4,000 cases; with the rule, zero at
4,000 (9,202 rows) and at 30,000 (70,083 rows), and the grammar and
indentation arms stay zero at both. 18 single-change mutants of the rule, the
operators, the scalaVersion gate and the meta-build flag were all killed. The
same benchmark measured 15.7 to 19.4 ms/op (median 16.4) against 15.7 to 20.0
ms/op (median 16.1) for the code before, run alternately at load average 14 to
17, with 16.45 MB/op and 139,757 to 139,758 allocs/op on both: no byte or
allocation change.

No-Regression Evidence (members anywhere, scalaVersion poison): the member arm
now places rewriting members outside statement position (a `val` body, after
`:+` or `++`, an `if` branch, a lambda body, a nested call), follows
`withRank(...)` with every operator class, and draws scalaVersion statements
the scanner cannot read in any order with literals. On the code before this
fix it reported state=2,167, count=2,170, scala=212 and hint=125 over 4,000
cases and state=16,502, count=16,096, scala=1,592 and hint=921 over 30,000;
with the fix, zero at 4,000 (9,542 rows) and at 30,000 (73,007 rows), and the
grammar and indentation arms stay zero with their row counts unchanged. 19
single-change overlay mutants of the member gate, the `withRank` operator
rule, the poison and the distinct-literal envelope were all killed. The same
benchmark, run alternately at load average about 5, measured 15.25 to 15.37
ms/op before and 15.38 to 15.41 ms/op after, with 16.45 MB/op and 139,757
allocs/op on both: no byte or allocation change.

No-Regression Evidence (stray scalaVersion poison, axis then member): the
member arm now expects every scalaVersion assignment outside a grammar
context (a settings `val` or `def`, `inThisBuild(...)`, a lambda, `in Test`,
`in (Test, compile)`, `.in(Test) :=`, `.:=(...)`) to poison the envelope,
draws key reads (`.value`, `.in(Test).value`) that must change nothing, and
places rewriting and read members after `.in(Test)`. On the code before this
fix it reported state=103, count=704, scala=208 and hint=232 over 4,000 cases
and state=764, count=5,144, scala=1,608 and hint=1,719 over 30,000; with the
fix, zero at 3,000 (7,171 rows) and at 30,000 (73,082 rows), and the grammar
and indentation arms stay zero with their row counts unchanged. 6 overlay
mutants (stray assignment counted without poison, `.:=(...)` as a read,
`.in(...)` then member as a read, the '.' after an axis left unconsumed,
`in (...)` group not skipped, `.in(...) :=` as a read) were all killed. The
same benchmark, run alternately at load average 8 to 9, measured 15.45 to
17.12 ms/op before and 15.46 to 16.43 ms/op after, with 16.45 MB/op and
139,757 to 139,758 allocs/op on both: no byte or allocation change.

No-Regression Evidence (default-deny key occurrences): the member arm now
draws parenthesized keys at depths 1 to 3, keys under a prefix axis in
parentheses, chained `in` axes, infix `set` and `withRank`, bare keys,
members that return no Setting, keys inside a fallback statement, and reads
and lookalikes that must change nothing (`.value` reads, `(k in Test).value`,
`myScalaVersion`, keys in strings and comments). On the code before this fix
it reported state=872, count=1,896, scala=138 and hint=133 over 3,000 cases
and state=9,195, count=19,037, scala=1,262 and hint=1,097 over 30,000; with
the fix, zero at 3,000 (5,569 rows) and at 30,000 (56,010 rows), and the
grammar and indentation arms stay zero with their row counts unchanged. 10
overlay mutants (unclaimed key not denied, a parenthesized key claimed for
either key, chained axes kept in context, an infix ident claimed for either
key, a non-read member claimed for either key, a watched read not benign,
`unread` not restoring the watch) were all killed; an end-of-input flush was
removed as an equivalent mutant, since the main loop consumes EOF through
`scan.next`. The same benchmark, run alternately at load average about 2,
measured 15.51 to 15.87 ms/op before and 15.85 to 16.19 ms/op after (about
3 percent), with 16.45 MB/op and 139,757 to 139,758 allocs/op on both: no
byte or allocation change.

No-Regression Evidence (watch read grammar): the member arm now draws reads
and writes of either key with axes and closers inside a statement the
fallback consumes (`(ThisBuild / k).value`, `(k in Test).value`,
`k.in(Test).value`, `(k in (Test, compile)).value`, and the same shapes ending
in `:=` or a non-read member) at five positions. On the code before this fix
it reported state=8, count=332, scala=8 and hint=7 over 3,000 cases and
state=59, count=3,173, scala=52 and hint=53 over 30,000; with the fix, zero
at 3,000 (5,589 rows) and at 30,000 (55,969 rows), and the grammar and
indentation arms stay zero with their row counts unchanged. 11 overlay
mutants (closer read, in-axis read and `.in(...)` read rejected, group end
read as a read, any member after closers, `.in` without a group, a nested
key in a group ignored, end-of-input `scalaVersion :=` uncounted, cover
ignored, never cleared, or set on a context reset) were all killed. The same
benchmark, run alternately at load average about 4 to 8, measured 18.77 to
21.10 ms/op (median 19.86) before and 17.49 to 20.95 ms/op (median 20.16)
after, with 16.45 MB/op and 139,758 to 139,759 allocs/op on both: no byte or
allocation change.

No-Regression Evidence: the bounded-input tests in `adversarial_test.go` run a
single-pass scanner with no recursion. 200,000 nested parentheses finish in
0.03 s, and 100,000 dependency lines finish in 0.08 s while emitting exactly
`MaxDependencyRows` (10,000) rows with the remaining 90,000 counted in
`sbt_state.dependency_overflow_count`. Backend: none (file bytes only, no
Postgres or graph access). Terminal counts: a 100,000-line file yields 10,000
rows, 90,000 overflow, 0 unresolved. The `git-collector` content version
constant stays `git-collector.v10` (set by #8965): the exact name moves the
registry fingerprint inside `GenerationContentVersion` on its own, so every
repository re-collects once. The pin re-records at 315 facts (311 before the
`build.sbt` fixture joined the pinned corpus).

No-Observability-Change: the package emits no metrics, spans, or logs, like the
Gradle, Maven, and go.mod parsers. Parse timing stays with the ingester and
collector runtime paths that call the parent Engine
(`eshu_dp_file_parse_duration_seconds`), and the bounded counts an operator
needs ride in the `sbt_state` payload (`unresolved_declaration_count`,
`dependency_overflow_count`, `scala_version_count`). An operator finds a file
that lost declarations by reading `sbt_state` on the content entity payload.
