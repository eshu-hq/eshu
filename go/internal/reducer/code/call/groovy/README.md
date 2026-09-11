# groovy

The Groovy call resolver (issue #6061). Wired into `code/call/languages.go`
under the `"groovy"` key. Not imported outside `code/call`.

## Ownership

Class-qualified, type-inferred callee resolution against the
repository-unique name table (`ReceiverType.method`, plus the call's own
qualified `full_name` when it already ends `.method`). Groovy does NOT use
the shared JVM imported-receiver resolver (`code/call/jvm`): it has no
import-bound receiver typing.

## Files

| File | Covers |
| --- | --- |
| `resolver.go` | `Resolvers`, `groovyClassQualifiedCandidateNames` |
| `doc.go` | Package contract |

## Dependency rule

Imports `code/call/shared` only. Never `code/call` or a sibling language
leaf.
