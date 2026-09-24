# AGENTS — go/internal/query/querycontract/rowvalue

## The one rule

**This package must stay a leaf.** It imports nothing from the query family and
must not start. Its whole reason for existing separately is that a handler-family
subpackage can import it without an import cycle back through the parent's
compatibility aliases.

Before adding an import here, ask whether the helper actually belongs in the
family that needs it. If it names a family type, it does.

## Changing a helper's contract is a wide change

Every function is total: no error return, no panic, zero value on a missing key,
a nil, or an unexpected type. Callers rely on that to degrade one field rather
than fail a request.

Changing what any of these returns is not a local edit, and the surface is
bigger than a file listing suggests. Measured at this head, run from the repo
root:

```
rg -o 'querycontract\.StringVal\(' -g '*.go' go | wc -l  -> 2174 calls
rg -l 'querycontract\.StringVal\(' -g '*.go' go | wc -l  ->  246 files
rg -o 'querycontract\.(StringVal|BoolVal|IntVal|StringSliceVal|FloatVal)\(' \
	-g '*.go' go | wc -l                             -> 2856 calls
rg -l  (same pattern, same flags)                        ->  283 files
```

Escape the paren: `rg` reads Rust regex, where a bare `(` opens a group instead
of matching one. All four are identical at `origin/main` `d3d4c2d3e`, because
the move touched no caller. At the older base `514534567` they read
2057 / 235 / 2705 / 272; that growth is #6060 families qualifying calls they
used to make in-package as they leave root, not new callers. Dropping the
receiver
(`rg -l '\.(StringVal|BoolVal|IntVal|StringSliceVal|FloatVal)\(' -g '*.go' go`)
reaches 316 files at this head and 314 at `origin/main` -- the two extra are
`querycontract/response_shaping_helpers.go`, whose forwarders now name the five
on the `rowvalue` receiver, and its test; not new callers.

Keep the `-g '*.go'` filter and the `go` path argument: without them, the prose
that documents the measurement is counted by it. The receiver-dropping command
reaches 317 files unfiltered, and the extra one is this file.

### Re-measuring a figure at an older commit

Every command above searches the working tree, so `rg` is the tool, as the root
`AGENTS.md` requires. The figures quoted at `d3d4c2d3e` and `514534567` are a
different job: `rg` searches a working tree and cannot read a commit, so
checking one needs `git grep` with a commit argument.

```
git grep -lE \
	'querycontract\.(StringVal|BoolVal|IntVal|StringSliceVal|FloatVal)\(' \
	d3d4c2d3e -- 'go/**/*.go' | wc -l                ->  283 files
```

That is a deliberate exception, not an oversight, and it is confined to
searching a named commit. The `rg`-only alternative is to `git worktree add` a
throwaway checkout at each commit and search that; it returns the same number
for the cost of a full checkout per commit compared, which is not worth paying
to re-check a figure. Translate carefully in either direction: `git grep -E` is
POSIX ERE, which has no `\b`, so a `\b`-anchored pattern silently matches
nothing there and reports zero rather than failing.

Budget an audit against the **call** count, not the file count -- 246 and 283
are files, and the call count is about 9x larger. Adding a case to `IntVal` or
`FloatVal` is usually safe; changing an existing case's result is not, and needs
the call sites audited.

`StringVal` renders a present non-string with `%v` while the others discard.
That asymmetry is intentional — see the README. Do not "fix" it for symmetry.

## Forwarders

Two packages forward into here, and they do not forward the same set.
`querycontract/response_shaping_helpers.go` forwards all five names.
`query/neo4j.go` forwards only four — `StringVal`, `BoolVal`, `IntVal` and
`StringSliceVal`. Package `query` has no exported `FloatVal`: it reaches this
one through two unexported wrappers, `floatVal` in `compare/handler.go` (10 call sites
in 2 files) and `relationshipFloatVal` in `repository_compat.go` (1 call site
in 1 file).

If you add a helper here, decide deliberately whether it also needs a forwarder,
and in which of those two packages; a new name with no existing callers needs
neither.

## The package name `rowvalue` is a settled exception

[docs/internal/naming.md](../../../../../docs/internal/naming.md) rule 3 would
push this directory toward the plain name `row`. The owner ruled that
`rowvalue` stays, because `row` does not survive contact with the call sites:
the parameter every one of these helpers takes is itself named `row`, so
`row.StringVal(row, k)` shadows the package selector and does not compile. The
ruling is recorded here so a later extraction does not reopen it as an
oversight. Renaming this package would mean renaming that parameter across
every caller for no contract gain.

## The file name follows rule 2; the package name is the only exception

[naming.md](../../../../../docs/internal/naming.md) rule 2 forbids repeating the
directory name in the file name, and rule 5 forbids carrying a stuttering name
into a new home. The implementation arrived here as `rowvalue.go`, which
stuttered against this directory the moment it moved, so #6597 renamed it to
`decode.go` (and its test to `decode_test.go`). `decode` is the verb the package
and its parent already use for this work -- doc.go calls it converting a driver's
untyped row into Go values, and the parent README calls these the "row-value
decoders" on a "hot row-decode loop". The owner ruled for the rename over the
in-tree `dir/dir.go` precedent: the rule was enshrined in ad16b2520 and this is
the first extraction after it.

The two rulings are separate and neither reopens the other. Rule 2 applies to
the file and was followed. Rule 3 would rename the package, and was waived for
the `row` shadowing reason above. Adding a file here needs a plain name for what
it does -- never `rowvalue_*.go`.
