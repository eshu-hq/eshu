# perl

The Perl call resolver (issue #6061). Wired into `code/call/languages.go`
under the `"perl"` key. Not imported outside `code/call`.

## Ownership

Package-import-bound callee resolution for `Package::function` calls: the
file must import the package by name, and the import path must uniquely
resolve the function name (or its trailing-name variants).

## Files

| File | Covers |
| --- | --- |
| `resolver.go` | `Resolvers`, package-qualified-call parsing, import-package matching |
| `doc.go` | Package contract |

## Dependency rule

Imports `code/call/shared` only. Never `code/call` or a sibling language
leaf.
