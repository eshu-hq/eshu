# Elixir Parser Agent Notes

Read `language.go`, `tree_sitter_syntax.go`, `dead_code_roots.go`, and
`helpers.go` first. Keep this package parent-independent: use
`internal/parser/shared` for payload, source, sorting, and common parser
helpers. Do not import `internal/parser`.

Preserve existing payload keys and sorting unless a parser contract change is
covered by tests and downstream materialization updates.
Tree-sitter metadata should augment function spans, signatures, decorators, and
module context without replacing bounded lexical call/dependency extraction.
Caller-owned parser entrypoints must keep parser ownership with the caller and
must not close parsers they did not create. Parser-backed reachability roots
must stay conservative: only emit
`dead_code_root_kinds` when Elixir syntax proves an Application, macro, guard,
behaviour, GenServer, Supervisor, Mix task, protocol, Phoenix controller, or
LiveView root. Validate callback arity where Elixir defines it. Use
`exactness_blockers` for observed dynamic dispatch rather than pretending the
call target is known.
