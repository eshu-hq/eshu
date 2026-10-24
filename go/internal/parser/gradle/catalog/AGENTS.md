# AGENTS.md - internal/parser/gradle/catalog

## Read first

1. `README.md` - the grammar table, the bounds, and what the parser never does.
2. `doc.go` - the godoc contract.
3. `scan.go` - `Scan`, the section walk, alias and header checks.
4. `entries.go` - library, plugin and version decoding, and the validators.
5. `rows.go` - version resolution and row shape.
6. `depth.go` - `nestingExceeds`, the guard that runs before the TOML parser,
   and `keyBudget`, the key-segment bounds.
7. `keys.go` - `keyTree`, which rejects any key or table defined twice.
8. The parent hook `../../gradle_language.go` and the registry entry in
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
  does not carry. Slots are keyed on `accessorKey` (Gradle's `toJavaName`
  with a lower-case first letter), so two aliases with one accessor name,
  which fail Gradle's build, collide too; a reference lookup still checks the
  normalized spelling (`sameNormalized`). Keep `aliasTable` to one map per
  table, with the exact spellings per slot and the rare-case maps used only by
  shared slots. Every check is one map operation: an alias has 3^k separator
  spellings, so a scan over earlier spellings is quadratic.
- Gradle walks each table's `keySet`. Every key it would register must enter
  the alias table, even when it emits no row: the first segment of a dotted
  key, the alias of a `[table.alias]` sub-table or `[[table.alias]]` array
  table, and an alias over `MaxFieldBytes`. Use `aliasTable.shadow`: a repeat
  of one exact spelling is a no-op, a new spelling shares the slot, and it
  counts toward `MaxRows`. Read keys with `keyHead`, which stops after three
  components, never with a slice of every component.
- Run `keyBudget.exceeded` on every top-level expression before the grammar
  and the key tree. It refuses a key with more than `MaxKeySegments` dotted
  segments and a file with more than `MaxFileKeySegments` in all, inline-table
  keys included, because the key tree allocates per segment.
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
- Any key or table TOML forbids defining twice makes the whole file
  `malformed` with no rows. `keyTree` (`keys.go`) checks every expression,
  in every table, the root and inline tables, with go-toml's tracker rules.
  Never go back to a go-toml decode for this: into an empty struct it skips
  every key under a table header (so a library defined twice parsed with
  rows), and into a map it stores every value, searches each table's keys in
  a list (11.6 s on 80,000 entries), and recurses once per dotted segment.
  Keep each check one map operation per segment; small inline tables use the
  list scan in `smallInline`, which counts the table first so nothing is
  checked twice. The test reference for "is this valid TOML" is
  `tomlRejects`, a map decode, in tests only.
- The package imports the standard library, `go-toml/v2/unstable` and
  `internal/parser/shared` only. Tests also import `go-toml/v2` for the
  `tomlRejects` reference.

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
a string notation one part short, drop the semantic TOML check (now the key
tree), miscount
`alias_collision_count`, and allow `$`. One is equivalent: letting a later
`[versions]` definition overwrite the first, because a shared slot never
resolves.

The nested-alias, accessor-name and key-segment set, all 18 killed: drop the
shadow call for a dotted key, a sub-table header, an array table, an over-long
alias, a `[versions]` alias or a `[bundles]` alias; make a repeated spelling
collide with itself; ignore a shadow past the cap or count its repeats twice;
stop counting a new shadow spelling toward the cap; key slots on the
normalized form; skip the capital after a separator; skip the normalized
check in `lookup`; make `sameNormalized` exact; move the segment bound by
one; ignore inline tables or arrays in `keysExceed`; and drop `keysExceed`
(now `keyBudget.exceeded`).

The key-tree and file-budget set, 30 of 31 killed: drop each rule (a table
defined twice, a repeated last key, a dotted key into a closed table, a dotted
key through an array table, a value as a dotted or header parent, `[[x]]` over
a table, `[x]` over an array table or a value); never mark a leaf a value;
reuse an array element's node; ignore the section of a dotted table, or close
it at once; skip the section count; drop the repeat or value check, or the
nested values, in `smallInline`; ignore the parent in `findInline` or the
index key; skip inline tables, arrays or the large-inline index path; drop
the key tree from `Scan`; move the file bound or the per-key bound by one;
stop counting inline keys toward the file bound; presize nothing; and accept a
repeated alias, a repeated catalog header or an exact repeat past the cap
(each still `malformed` through the key tree, killed by its reason text). One
is equivalent: `maxInlineKeys = 0` sends every inline table through the index,
which gives the same answers more slowly.
