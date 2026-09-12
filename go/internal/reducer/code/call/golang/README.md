# golang

The Go call resolver (issue #6061). Package name `golang`, not `go` (a
keyword) — the same precedent as `internal/parser/golang`. Wired into
`code/call/languages.go` under the `"go"` key. Not imported outside
`code/call`.

## Ownership

Package-qualified import binding (`pkg.Func`, bounded to same-repo source
directories), Go method-return-chain inference (`ctx.Actions().GetX()`
chains), same-directory unqualified resolution, and cross-repo package-export
resolution (an exported top-level function resolved across repositories via
`shared.EntityIndex`'s Go export index).

## Files

| File | Covers |
| --- | --- |
| `resolver.go` | `Resolvers` (phase-ordered list), the four resolve-function adapters |
| `imports.go` | Package-qualified/method-return-chain/same-directory `*EntityID` helpers, Go import-metadata parsing |
| `exports.go` | Cross-repo package-export callee resolution |
| `doc.go` | Package contract |

## Dependency rule

Imports `code/call/shared` only. Never `code/call` or a sibling language
leaf.
