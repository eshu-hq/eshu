# C# Parser Agent Notes

Read `language.go` first for payload flow, then `dead_code_roots.go` for
C# reachability evidence. Keep this package parent-independent: use
`internal/parser/shared` for payload, source, sorting, and tree-sitter node
helpers. Do not import `internal/parser`.

Preserve existing payload keys and sorting unless a parser contract change is
covered by tests and downstream materialization updates.

C# dead-code roots are parser evidence, not a full semantic model. Keep roots
bounded to syntax this package sees directly: local interface declarations,
same-file base lists, method attributes, explicit overrides, ASP.NET controller
shape, and hosted-service base types.
