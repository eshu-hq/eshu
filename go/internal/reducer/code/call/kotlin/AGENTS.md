# kotlin — agent instructions (issue #6061)

Read `code/call/jvm/AGENTS.md` first: this package is a thin binding of the
shared JVM receiver resolver to Kotlin's import/file-layout conventions.

## Invariants

- Never import `code/call` or a sibling language leaf other than
  `code/call/jvm`.
- `kotlinReceiverResolverConfig` must keep `MatchTypeFileName: false` and
  both `"import"` and `"alias"` in `ImportTypes` — Kotlin allows a type to be
  declared in any file, and aliased imports (`import a.b.Service as Svc`)
  are common; dropping either regresses real resolution, not just tests.
- Prove changes with `go test ./internal/reducer/code/call/... -count=1`
  and the extensive Kotlin resolver/alias-chain/constructor/scope-function/
  smart-cast/suspend test files in `code/call` (the largest single-language
  test surface in the family).
