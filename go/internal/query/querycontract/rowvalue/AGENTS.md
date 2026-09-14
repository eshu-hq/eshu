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
bigger than a file listing suggests. Measured at the base `514534567`:

```
git grep -o  'querycontract\.StringVal('                          -> 2057 calls
git grep -l  'querycontract\.StringVal('                          ->  235 files
git grep -oE 'querycontract\.(StringVal|BoolVal|IntVal|StringSliceVal|FloatVal)\(' -> 2705 calls
git grep -lE  (same pattern)                                      ->  272 files
```

each with the pathspec `-- 'go/**/*.go'`. Dropping the receiver
(`git grep -lE '\.(StringVal|BoolVal|IntVal|StringSliceVal|FloatVal)\('`)
reaches 303 files at that base, and 305 at this head -- the two extra are the
new `rowvalue` forwarder file and its test, not new callers.

The four qualified figures are identical at the base and at this head, because
the move touched no caller. Keep the `*.go` pathspec: without it, the prose that
documents the measurement is counted by it.

Budget an audit against the **call** count, not the file count -- 235 and 272
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
one through two unexported wrappers, `floatVal` in `compare.go` (12 call sites
in 3 files) and `relationshipFloatVal` in `repository_compat.go` (2 call sites
in 2 files).

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
