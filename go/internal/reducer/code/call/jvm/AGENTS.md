# jvm — agent instructions (issue #6061)

Read `README.md` before changing `ResolveReceiverCallee`: both `java` and
`kotlin` depend on its exact behavior, keyed only by the `ReceiverConfig`
each passes.

## Invariants

- Never import `code/call`, `code/call/java`, or `code/call/kotlin`. This
  package sits below both; an import back up would be a cycle.
- `ReceiverConfig.MatchTypeFileName` is the load-bearing difference between
  Java (`true`, one public class per file) and Kotlin (`false`, a type may
  live in any file). Do not default one language's config onto the other.
- An import binding that is *attempted* but ambiguous (more than one
  candidate path) must return `("", true)` from
  `resolveImportedReceiverCallee`/`importedReceiverPaths`, not fall through
  to the type-inference candidate names — `ImportedReceiverBlocksRepoFallback`
  depends on that `attempted` boolean to block the broad repo-unique-name
  guess.
- Prove changes with `go test ./internal/reducer/code/call/... -count=1`
  (both `java` and `kotlin`'s resolver/receiver-import tests exercise this
  package).
