# kotlin

The Kotlin call resolver (issue #6061). Wired into `code/call/languages.go`
under the `"kotlin"` key. Not imported outside `code/call`.

## Ownership

Binds the shared JVM imported-receiver resolver (`code/call/jvm`) to
Kotlin's parser output: both plain and aliased (`import a.b.C as D`) imports
introduce types, dotted package paths map to `.kt` source files, and there is
no filename-matching requirement since Kotlin allows a type to live in any
file (the prescan import map already points the declared type at its real
file).

## Files

| File | Covers |
| --- | --- |
| `resolver.go` | `Resolvers`, `kotlinReceiverResolverConfig`, `BlocksRepoFallback` |
| `doc.go` | Package contract |

## Dependency rule

Imports `code/call/shared` and `code/call/jvm` only. Never `code/call` or a
sibling language leaf other than `jvm`.
