# Elixir Parser Agent Notes

Read `language.go`, `ast_extract.go`, `ast_calls.go`, `ast_nodes.go`,
`ast_shared.go`, `dead_code_roots.go`, and `helpers.go` first. Keep this package
parent-independent: use `internal/parser/shared` for payload, source, sorting,
and common parser helpers. Do not import `internal/parser`.

All Elixir source-symbol extraction is tree-sitter AST based. Do not reintroduce
regex or line-scan extraction of modules, functions, imports, attributes, or
calls; key every row by AST node spans. The only manifest parsing is
`hex_dependencies.go` for `mix.exs`/`mix.lock`, which is a structured-format
manifest, not Elixir source.

Preserve existing payload keys and sorting unless a parser contract change is
covered by tests and downstream materialization updates. A call row requires a
parenthesized argument list; one-line `def ..., do:` bodies do not emit calls.
Caller-owned parser entrypoints must keep parser ownership with the caller and
must not close parsers they did not create. Parser-backed reachability roots
must stay conservative: only emit `dead_code_root_kinds` when Elixir syntax
proves an Application, macro, guard, behaviour, GenServer, Supervisor, Mix task,
protocol, Phoenix controller, or LiveView root. Validate callback arity where
Elixir defines it. Use `exactness_blockers` for observed dynamic dispatch rather
than pretending the call target is known.
