# Scala Parser Agent Notes

Read `language.go` first, then `dead_code_roots.go`. Keep this package
parent-independent: use `internal/parser/shared` for payload, source, sorting,
and tree-sitter node helpers. Do not import `internal/parser`.

Preserve existing payload keys and sorting unless a parser contract change is
covered by tests and downstream materialization updates.

Dead-code roots must stay syntax-backed. Do not root broad public Scala API
surfaces, all controller methods, implicits/givens, macros, Play route-file
entries, or compiler-plugin output without a dedicated parser/query design and
fixture coverage.
