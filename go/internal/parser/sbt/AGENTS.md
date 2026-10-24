# AGENTS.md - internal/parser/sbt guidance

## Read first

1. `README.md` - purpose, row contract, bounds, and exclusions.
2. `doc.go` - godoc contract.
3. `lexer.go` - the single-pass tokenizer (comments, strings, interpolators).
4. `parser.go` - `Parse`, the main dispatch loop, `sbt_state`, val resolution.
5. `declaration.go` - `libraryDependencies` operator, collection, and element
   parsing, and the resynchronisation helpers.
6. `scope.go` - configuration list splitting and the scope mapping.
7. `trailing.go` - a trailing `% config`, collection `.map(_ % config)`, and
   the shared element skipper.
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
- A scope the scanner cannot map is `partial` and counted in
  `unresolved_declaration_count`; it is never a resolved `compile`. Add a new
  configuration name to `configPartScope` with a test, or leave it unknown.
- `dependency_resolution_state` stays in the `resolved`/`partial`/`unresolved`
  vocabulary the reducer and docs depend on.
- `name` is `organization:name` as written. Do not append the Scala suffix to
  it; `sbt_binary_artifact` carries the derived artifact name.
- Emitted rows and `sbt_state` are deterministic: rows sort by name with a
  stable sort, and no map iteration order reaches the output.

## Common changes and how to scope them

- Add a failing case to `parser_test.go` before changing the scanner, with a
  positive and a negative fixture.
- A new modifier on a coordinate belongs in `skipElement` (it is skipped, not
  evaluated) unless it changes identity or scope.
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
(`Engine.ParsePath`, linux/arm64, Go 1.26). Input shape: a synthetic manifest
of about 10,000 lines (10,001 for Gradle, 10,000 for sbt), one literal
dependency per line. After: sbt takes 13.7 to 14.4 ms/op (52.6 to 55.3 MB/s),
16.4 MB/op, 139,756 allocs/op; Gradle takes 56.8 to 58.2 ms/op (10.8 to
11.1 MB/s), 12.8 MB/op, 119,743 to 119,753 allocs/op. The sbt parser is about 4.0 times
faster on this shape and allocates about 29 percent more bytes, because it
builds one row map per dependency and keeps each token's source slice. This is
a new parser on a path no existing file reaches (`build.sbt` was unadmitted),
so existing files see no change.

No-Regression Evidence: the bounded-input tests in `adversarial_test.go` run a
single-pass scanner with no recursion. 200,000 nested parentheses finish in
0.02 s, and 100,000 dependency lines finish in 0.06 s while emitting exactly
`MaxDependencyRows` (10,000) rows with the remaining 90,000 counted in
`sbt_state.dependency_overflow_count`. Backend: none (file bytes only, no
Postgres or graph access). Terminal counts: a 100,000-line file yields 10,000
rows, 90,000 overflow, 0 unresolved. The `git-collector` content version
constant stays `git-collector.v9`: the exact name moves the registry
fingerprint inside `GenerationContentVersion` on its own, so every repository
re-collects once. The pin re-records at 297 facts (293 before the `build.sbt`
fixture joined the pinned corpus).

No-Observability-Change: the package emits no metrics, spans, or logs, like the
Gradle, Maven, and go.mod parsers. Parse timing stays with the ingester and
collector runtime paths that call the parent Engine
(`eshu_dp_file_parse_duration_seconds`), and the bounded counts an operator
needs ride in the `sbt_state` payload (`unresolved_declaration_count`,
`dependency_overflow_count`, `scala_version_count`). An operator finds a file
that lost declarations by reading `sbt_state` on the content entity payload.
