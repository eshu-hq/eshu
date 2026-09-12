# swift — agent instructions (issue #6061)

Read `code/call/shared/AGENTS.md` first, especially `receiver_method_index.go`
— this package is a one-line binding to `shared.ResolveReceiverMethodCallee`,
shared with `code/call/javascript`.

## Invariants

- Never import `code/call` or a sibling language leaf.
- Do not add Swift-specific receiver logic here without first checking
  whether it belongs in `shared.ResolveReceiverMethodCallee` instead — that
  function is also JavaScript's receiver resolver
  (`receiverMethodLanguage` gates on `"swift"`, `"javascript"`, `"jsx"`), so
  a Swift-only change to the shared function needs to prove it does not
  affect JavaScript resolution.
- Prove changes with `go test ./internal/reducer/code/call/... -count=1`
  and the Swift resolver tests in `code/call`.
