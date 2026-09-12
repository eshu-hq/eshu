# java — agent instructions (issue #6061)

Read `code/call/jvm/AGENTS.md` first: this package is a thin binding of the
shared JVM receiver resolver to Java's import/file-layout conventions.

## Invariants

- Never import `code/call` or a sibling language leaf other than
  `code/call/jvm`.
- `javaReceiverResolverConfig` must keep `MatchTypeFileName: true` — Java
  enforces one public class per file matching the filename, and
  `jvm.ResolveReceiverCallee` relies on that to disambiguate package
  directories. Kotlin's config (in `code/call/kotlin`) differs precisely
  here; do not copy it.
- Prove changes with `go test ./internal/reducer/code/call/... -count=1`
  and the Java resolver/receiver/metadata/reflection/overload tests in
  `code/call`.
