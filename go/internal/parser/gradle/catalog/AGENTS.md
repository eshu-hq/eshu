# AGENTS.md - internal/parser/gradle/catalog

## Read first

1. `README.md` - the grammar table, the bounds, and what the parser never does.
2. `doc.go` - the godoc contract.
3. `scan.go` - `Scan`, the section walk, alias and header checks.
4. `entries.go` - library, plugin and version decoding, and the validators.
5. `rows.go` - version resolution and row shape.
6. `depth.go` - `nestingExceeds`, the guard that runs before the TOML parser.
7. The parent hook `../../gradle_language.go` and the registry entry in
   `../../registry_definitions.go`.

## Invariants this package enforces

- Fail closed. A `resolved` row carries the coordinate and one exact version
  exactly as written in the file, or as written in the `[versions]` string it
  references. Any other case is `partial` (no version), `unresolved` (a version
  the grammar cannot read, a selector such as `1.+`, `latest.release`, a range,
  `!!` or `@jar`, or a colliding alias), or no row. Never invent, normalize,
  trim or truncate a value.
- Match aliases the way Gradle does: `-`, `_` and `.` are one separator, and
  a `version.ref` is normalized before its lookup. Never pick the survivor of a
  normalized collision; Gradle's choice depends on registration order the file
  does not carry. Keep `aliasTable` to one map per table, with the exact
  spellings per slot and the rare-case maps used only by shared slots. Every
  check is one map operation: an alias has 3^k separator spellings, so a scan
  over earlier spellings is quadratic.
- Run `nestingExceeds` before the TOML parser. The parser has no depth bound
  and overflows the stack on deeply nested input. Do not move the check after
  `parser.Reset`.
- Keep every loop linear in the input. `lineAt` counts newlines
  incrementally; do not use `Parser.Shape`, which rescans from the start of the
  file on each call. Keep the file, row, depth and field bounds in `types.go`
  and count what they drop.
- Plugin rows use `config_kind=gradle_plugin`. Only `dependency` is admitted
  by the package reducer, and a plugin id is not a Maven coordinate.
- Rows set no `direct_dependency` and no `dependency_scope`: a catalog entry
  declares, it does not use.
- A repeated table header, an exactly repeated alias, or input the semantic
  TOML decode rejects makes the whole file `malformed`. The decode runs after
  the streaming parse, never before it: on an unterminated string it copies the
  rest of the file before reporting the error. Keep its target an empty
  struct. The decoder recurses once per dotted-key segment into a field or map
  it fills, and `nestingExceeds` does not count dotted keys, so a map target
  overflows the stack on a long dotted key.
- The package imports the standard library, `go-toml/v2`, `go-toml/v2/unstable`,
  and `internal/parser/shared` only.

## Common changes

- A new accepted construct: write the negative and positive fixture in
  `scan_test.go` first, add the construct to the README grammar table, and add
  a generator case with its oracle outcome in `fuzz_test.go`. Run the mutation
  checks below; a widened grammar must kill a test.
- A new bound: add the constant to `types.go`, count the excess, and add an
  allocation-ceiling test in `bounds_test.go`.
- A change to row fields: update the README, the parent engine test
  `engine_test.go`, and the re-pinned collector fixture
  digest (`go test ./internal/collector/repo/git/ -run
  TestGenerationContentVersionPinsCollectorOutput -update-content-version-pin`).
  A row field change for unchanged input also needs a
  `gitGenerationContentVersion` bump.

## Mutation checks

Run each mutant with `go test -overlay` against a copy of one source file and
expect a test failure. The set that was run and killed: allow `$` in version
text, resolve a reference to a missing key, accept unknown inline-table keys,
accept four-part notation, disable the row cap, accept a space in a coordinate
part, accept a repeated alias, stop the line counter, ignore the size bound,
accept a repeated header, loosen the depth limit, and break the string skipping
in `nestingExceeds`. One mutant (resolve a reference to a rich version) is
equivalent: a rich version stores no text, so `validVersionText` still rejects
it.

The collision and selector set, also killed: turn off alias normalization,
ignore a library or a `[versions]` collision, stop counting later spellings or
dropped aliases, forget a later spelling (so its exact repeat passes), accept
an exact repeat past the cap, look up a reference
without normalizing it, allow each of `+`, `latest.`, `!`, `@` and the range
characters, use `validVersionText` for a literal or a reference target, accept
a string notation one part short, drop the semantic TOML check, miscount
`alias_collision_count`, and allow `$`. One is equivalent: letting a later
`[versions]` definition overwrite the first, because a shared slot never
resolves.
