# AGENTS.md - internal/parser/sbt guidance

## Read first

1. `README.md` - purpose, row contract, bounds, and exclusions.
2. `doc.go` - godoc contract.
3. `lexer.go` - the single-pass tokenizer (comments, strings, interpolators).
4. `parser.go` - `Parse`, the main dispatch loop, `sbt_state`, val resolution.
5. `declaration.go` - `libraryDependencies` operator, collection, and element
   parsing, and the resynchronisation helpers.
6. `rows.go` - scope mapping, Scala binary version, and row construction.
7. `../gradle/AGENTS.md` - the sibling manifest parser whose row contract this
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

No-Observability-Change: this package emits no metrics, spans, or logs, like
the Gradle, Maven, and go.mod parsers. Parse timing stays with the ingester
and collector runtime paths that call the parent Engine, and the bounded
counts ride in the `sbt_state` payload for an operator to read.
