# rust

The Rust call resolver (issue #6061). Wired into `code/call/languages.go`
under the `"rust"` key. Not imported outside `code/call`.

## Ownership

Trait-bound receiver-typed callee resolution: finds the containing function
of a receiver-typed call, reads its `where` clause for trait bounds on the
receiver's type, and resolves the called method against
`shared.EntityIndex.RustTraitMethodsByRepo`'s trait-method table.

## Files

| File | Covers |
| --- | --- |
| `resolver.go` | `Resolvers`, `where`-predicate parsing, trait-bound derivation |
| `doc.go` | Package contract |

## Dependency rule

Imports `code/call/shared` only. Never `code/call` or a sibling language
leaf.
