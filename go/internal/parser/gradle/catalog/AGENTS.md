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

- Fail closed. A `resolved` row carries the coordinate and version exactly as
  written in the file, or as written in the `[versions]` string it references.
  Any other case is `partial` (no version), `unresolved` (a version the
  grammar cannot read), or no row. Never invent, normalize, trim or truncate a
  value.
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
- A repeated table header or alias makes the whole file `malformed`.
- The package imports the standard library, `go-toml/v2/unstable`, and
  `internal/parser/shared` only.

## Common changes

- A new accepted construct: write the negative and positive fixture in
  `scan_test.go` first, add the construct to the README grammar table, and add
  a generator case with its oracle outcome in `fuzz_test.go`. Run the mutation
  checks below; a widened grammar must kill a test.
- A new bound: add the constant to `types.go`, count the excess, and add an
  allocation-ceiling test in `bounds_test.go`.
- A change to row fields: update the README, the parent engine test
  `../../gradle_catalog_engine_test.go`, and the re-pinned collector fixture
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
