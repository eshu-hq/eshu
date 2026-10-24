# AGENTS.md - internal/parser/sbt guidance

## Read first

1. `README.md` - purpose, row contract, bounds, and exclusions.
2. `doc.go` - godoc contract.
3. `lexer.go` - the single-pass tokenizer (comments, strings, interpolators).
4. `parser.go` - `Parse`, the main dispatch loop, `sbt_state`, val resolution.
5. `statement.go` - the statement entry (`parseLibraryDependencies`), the two
   grammar contexts (`keyContext`), the statement end, the fail-closed
   fallback entry (`failStatement`), and superseding (`markReset`).
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
- `TestGrammarSeededFuzzInvariants` (`fuzz_test.go`) is the regression net:
  both arms, grammar and indentation (`fuzz_indent_test.go`), must report zero
  I1, I2, I3, missing and fabricated cases. Run it with
  `SBT_GRAMMAR_FUZZ_CASES=30000` after a grammar change. Keep the grammar
  arm's draws unchanged so a seed reproduces the earlier review runs.
- The bare file is the column-0 lines only (`token.lineAtCol0`). sbt 2.x reads
  build.sbt as Scala 3, so an indented line at depth zero is a braceless block:
  never a grammar context, and never a tracked `val`. Do not relax this to
  "depth zero" alone.
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
- Plugin coordinates (`addSbtPlugin`) and `project/*.sbt` files are a separate
  admission and fact decision, not a change here.
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
one row map per dependency. A 120-byte `dependency` struct measured 18.1 MB/op
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
stays zero.

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
