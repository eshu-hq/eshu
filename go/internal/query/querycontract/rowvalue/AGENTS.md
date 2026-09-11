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

`querycontract/response_shaping_helpers.go` and `query/neo4j.go` forward these
names. If you add a helper here, decide deliberately whether it also needs a
forwarder; a new name with no existing callers does not.
