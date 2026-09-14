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

`querycontract` keeps forwarding wrappers for all five names, so existing
callers compile unchanged. That surface is large: at the base `514534567`,
`StringVal` alone is called 2057 times across 235 files, and the five together
2705 times across 272 files. The exact commands are in
[AGENTS.md](AGENTS.md#changing-a-helpers-contract-is-a-wide-change). The move
changed no caller, so the same commands give the same numbers at this head --
excluding the prose that documents them, which a `go/**/*.go` pathspec keeps
out of the Go files and a Markdown file cannot be counted by at all.

Package `query` forwards only four of them — `StringVal`, `BoolVal`, `IntVal`
and `StringSliceVal`, in `neo4j.go`. It has no exported `FloatVal`; it reaches
this one through two unexported wrappers, `floatVal` in `compare.go` and
`relationshipFloatVal` in `repository_compat.go`.

Every one of those wrappers inlines away, including the ones `querycontract`
added when these functions moved here. The exception is `StringVal` itself,
whose `%v` fallback puts its body over the inliner's cost budget — it did so in
its old home too, so the move changed nothing. The measured costs are in
[the parent README](../README.md#performance).
