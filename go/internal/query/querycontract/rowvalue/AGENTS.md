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

Changing what any of these returns is not a local edit. At the time of the
extraction `StringVal` had 235 qualified call sites and the five helpers were
named in 285 files. Adding a case to `IntVal` or `FloatVal` is usually safe;
changing an existing case's result is not, and needs the call sites audited.

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
