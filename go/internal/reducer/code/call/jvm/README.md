# jvm

The shared JVM imported-receiver resolver (issue #6061), used by
`code/call/java` and `code/call/kotlin`. `code/call/groovy` does NOT use it
(Groovy has no import-bound receiver typing). Not imported outside
`code/call/java` and `code/call/kotlin`.

## Ownership

Binds a receiver-typed call (`inferred_obj_type` plus a `class_context` on
the declaration) to the declaration of an imported type when the import
resolves to exactly one repository path, then falls back to
repository-scoped type-inference candidate names. Java and Kotlin differ
only in the `ReceiverConfig` they pass: which `import_type` values introduce
a binding, the source-file extension, and whether the file must be named
after the type.

## Files

| File | Covers |
| --- | --- |
| `receiver.go` | `ReceiverConfig`, `ResolveReceiverCallee`, `ImportedReceiverBlocksRepoFallback` |
| `doc.go` | Package contract |

## Dependency rule

Imports `code/call/shared` only. Never `code/call`, `code/call/java`, or
`code/call/kotlin`.
