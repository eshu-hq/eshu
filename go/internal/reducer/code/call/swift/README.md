# swift

The Swift call resolver (issue #6061). Wired into `code/call/languages.go`
under the `"swift"` key. Not imported outside `code/call`.

## Ownership

Receiver-typed method resolution via the shared repo-scoped receiver-method
index (`shared.ResolveReceiverMethodCallee`). Swift imports name modules,
not files, so there is no import-to-file binding available; resolution is
repo-scoped type inference only.

## Files

| File | Covers |
| --- | --- |
| `resolver.go` | `Resolvers` |
| `doc.go` | Package contract |

## Dependency rule

Imports `code/call/shared` only. Never `code/call` or a sibling language
leaf.
