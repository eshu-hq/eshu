# typescript

The TypeScript/TSX call resolver (issue #6061). Wired into
`code/call/languages.go` under both the `"typescript"` and `"tsx"` keys.
Not imported outside `code/call`. `internal/parser` keeps TypeScript nested
inside its `javascript` package; this family keeps them separate leaves
because the resolvers are independent (TypeScript resolves against declared
interface methods; JavaScript resolves against inferred receiver classes and
dynamic aliasing).

## Ownership

Interface-typed method resolution: a receiver's simple interface name
(rejecting union/intersection/generic/array-shaped types) resolved against
`shared.EntityIndex`'s declared-interface-implementer-method table.

## Files

| File | Covers |
| --- | --- |
| `resolver.go` | `Resolvers`, `typeScriptSimpleInterfaceName` |
| `doc.go` | Package contract |

## Dependency rule

Imports `code/call/shared` only. Never `code/call`, `code/call/javascript`,
or another sibling language leaf.
