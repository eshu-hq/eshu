# dart

The Dart call resolver (issue #6061). Wired into `code/call/languages.go`
under the `"dart"` key. Not imported outside `code/call`.

## Ownership

Import-bound callee resolution for both `package:` and relative Dart
imports, and the repo-fallback-blocking check for an explicit import that
names a call target without uniquely resolving it.

## Files

| File | Covers |
| --- | --- |
| `resolver.go` | `Resolvers`, `BlocksRepoFallback`, import-path candidate derivation |
| `doc.go` | Package contract |

## Dependency rule

Imports `code/call/shared` only. Never `code/call` or a sibling language
leaf.
