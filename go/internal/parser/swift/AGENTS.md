# Swift Parser Agent Notes

Read `language.go` first. Keep this package parent-independent: use
`internal/parser/shared` for payload, source, sorting, and common parser
helpers. Do not import `internal/parser`.

Preserve existing payload keys and sorting unless a parser contract change is
covered by tests and downstream materialization updates.

Dead-code reachability hints belong in parser metadata as
`dead_code_root_kinds`. Keep Swift root modeling bounded to syntax and
same-file evidence that this package can prove without importing the parent
parser.
