# javascript

The JavaScript/JSX call resolver (issue #6061). Wired into
`code/call/languages.go` under both the `"javascript"` and `"jsx"` keys.
Not imported outside `code/call`.

## Ownership

Receiver-typed method resolution (via `shared.ResolveReceiverMethodCallee`),
dynamic (alias-obscured) call resolution — static local aliasing and
destructuring patterns that look dynamic in call metadata but have a literal
same-file target — and the file-root/top-level-reference caller identity
helpers `code/call` uses for module-body and route-configuration calls
(shared across JavaScript, JSX, TypeScript, and TSX).

## Files

| File | Covers |
| --- | --- |
| `resolver.go` | `Resolvers` |
| `dynamic.go` | `ResolveDynamicCallee`, static-alias lookup, member-expression normalization |
| `roots.go` | `FileRootCallerID`, `SameFileTopLevelCallerID`, `TopLevelReferenceCallerID` |
| `doc.go` | Package contract |

## Dependency rule

Imports `code/call/shared` only. Never `code/call` or a sibling language
leaf (including `code/call/typescript`, despite the shared root-caller
logic — TypeScript's own resolver lives separately).
