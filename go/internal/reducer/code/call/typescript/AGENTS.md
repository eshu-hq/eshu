# typescript — agent instructions (issue #6061)

Read `code/call/shared/AGENTS.md` first for the
`TypeScriptInterfaceMethodsByRepo` accessor this package depends on.

## Invariants

- Never import `code/call`, `code/call/javascript`, or another sibling
  language leaf, even though TypeScript is JavaScript-family for language
  detection elsewhere in `shared`.
- `typeScriptSimpleInterfaceName` must keep rejecting any receiver type
  string containing `|&<>{}[]().,` — a union, intersection, generic, tuple,
  or call-signature type is never a simple interface name, and treating one
  as such would silently mismatch the declared-interface table.
- Prove changes with `go test ./internal/reducer/code/call/... -count=1`
  and the TypeScript resolver/baseurl/direct-import/type-reference tests in
  `code/call`.
