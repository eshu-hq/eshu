# Elixir Parser Agent Notes

Read `language.go`, `dead_code_roots.go`, and `helpers.go` first. Keep this
package parent-independent: use `internal/parser/shared` for payload, source,
sorting, and common parser helpers. Do not import `internal/parser`.

Preserve existing payload keys and sorting unless a parser contract change is
covered by tests and downstream materialization updates.
Parser-backed reachability roots must stay conservative: only emit
`dead_code_root_kinds` when Elixir syntax proves an application, escript,
macro, guard, behaviour, GenServer, Supervisor, Mix task, protocol, Phoenix
controller, or LiveView root. Use `exactness_blockers` for observed dynamic
dispatch rather than pretending the call target is known.
