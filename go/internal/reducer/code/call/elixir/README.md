# elixir

The Elixir call resolver (issue #6061). Wired into `code/call/languages.go`
under the `"elixir"` key. Not imported outside `code/call`.

## Ownership

Alias-import-bound callee resolution for qualified `Module.function` calls
(including nested-alias suffixes, e.g. `alias Foo.Bar` then `Bar.Baz.call`),
and the repo-fallback-blocking check for an explicit alias binding.

## Files

| File | Covers |
| --- | --- |
| `resolver.go` | `Resolvers`, `BlocksRepoFallback`, alias-module resolution |
| `doc.go` | Package contract |

## Dependency rule

Imports `code/call/shared` only. Never `code/call` or a sibling language
leaf.
