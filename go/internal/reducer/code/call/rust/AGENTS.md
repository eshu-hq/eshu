# rust — agent instructions (issue #6061)

Read `code/call/shared/AGENTS.md` first for the `RustTraitMethodsByRepo`
accessor this package depends on.

## Invariants

- Never import `code/call` or a sibling language leaf.
- The resolver must resolve to nothing when more than one trait bound
  yields a match (`len(matches) != 1`) — a generic receiver with several
  trait bounds that each declare the called method name is genuinely
  ambiguous, not resolvable by picking the first bound.
- `rustContainingFunctionItem` picks the narrowest containing span by width,
  matching the same "narrowest span wins" convention `shared`'s
  `ResolveContainingEntityID` uses; keep them consistent if either changes.
- Prove changes with `go test ./internal/reducer/code/call/... -count=1`
  and the Rust resolver tests in `code/call`.
