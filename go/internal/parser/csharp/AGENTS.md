# C# Parser Agent Notes

Read `language.go` first. Keep this package parent-independent: use
`internal/parser/shared` for payload, source, sorting, and tree-sitter node
helpers. Do not import `internal/parser`.

Preserve existing payload keys and sorting unless a parser contract change is
covered by tests and downstream materialization updates.
