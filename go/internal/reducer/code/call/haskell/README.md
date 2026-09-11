# haskell

The Haskell call resolver (issue #6061). Wired into `code/call/languages.go`
under the `"haskell"` key. Not imported outside `code/call`.

## Ownership

Qualified-import-bound callee resolution for `Module.function` calls,
matching against a file's `import`/aliased-`import` declarations.

## Files

| File | Covers |
| --- | --- |
| `resolver.go` | `Resolvers`, `QualifiedImportTargetExists`, qualified-call parsing |
| `doc.go` | Package contract |

## Dependency rule

Imports `code/call/shared` only. Never `code/call` or a sibling language
leaf.
