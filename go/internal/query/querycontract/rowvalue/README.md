# rowvalue

Type-asserting accessors for driver result rows.

## Why it exists

Graph and SQL drivers return a row as `map[string]any`. Without a shared
accessor, every read path writes its own type switch, and they disagree about
what a missing column means. These five functions settle that once:

| helper | returns on a missing key, nil, or wrong type |
| --- | --- |
| `StringVal` | `""` — except a non-string *present* value, which is rendered with `%v` |
| `BoolVal` | `false` |
| `IntVal` | `0` — accepts `int64`, `int`, `float64` |
| `StringSliceVal` | `nil` — accepts `[]string` and `[]any`, skipping non-string elements |
| `FloatVal` | `0` — accepts `float64`, `float32`, `int`, `int64` |

The asymmetry in `StringVal` is deliberate. A driver that returns a number where
a string was expected still carries the value the caller asked for, so rendering
it is more useful than discarding it. The others have no such rendering, because
a non-bool or a non-numeric carries no usable value for those shapes.

## Degrade the field, not the request

Every helper is total: no error return, no panic. A graph read that lost one
column should degrade that field rather than fail the whole response. Callers
that need to distinguish "absent" from "zero" must check the row themselves —
these helpers deliberately collapse the two.

## It is a leaf, and that is load-bearing

This package imports nothing from the query family. That property is why it can
be extracted at all: `#6060` moves each handler family into its own subpackage,
and a subpackage cannot import the root package back without an import cycle
through root's compatibility aliases. A family package can import `rowvalue`
freely.

Adding a dependency on a query-family type here would re-create that cycle for
every package downstream. If a helper needs a family type, it belongs in that
family, not here.

## Compatibility

`querycontract` and package `query` both keep forwarding wrappers under the
original names, so existing callers compile unchanged. At the time of the move
`StringVal` alone had 235 qualified call sites and the five helpers were named
in 285 files.
