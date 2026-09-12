# java

The Java call resolver (issue #6061). Wired into `code/call/languages.go`
under the `"java"` key. Not imported outside `code/call`.

## Ownership

Binds the shared JVM imported-receiver resolver (`code/call/jvm`) to Java's
parser output: only `import` declarations introduce types, and dotted
package paths map to `.java` source files matching the declared type's name.
Also provides the file-root caller identity for
`service_loader_provider`/`spring_autoconfiguration_class` references.

## Files

| File | Covers |
| --- | --- |
| `resolver.go` | `Resolvers`, `javaReceiverResolverConfig`, `BlocksRepoFallback` |
| `roots.go` | `MetadataFileRootCallerID` |
| `doc.go` | Package contract |

## Dependency rule

Imports `code/call/shared` and `code/call/jvm` only. Never `code/call` or a
sibling language leaf other than `jvm`.
