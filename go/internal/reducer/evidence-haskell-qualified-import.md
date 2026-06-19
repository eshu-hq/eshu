# Haskell Qualified Import Evidence

No-Regression Evidence: `go test ./internal/reducer -run 'Haskell.*Import' -count=1`
failed before `import qualified Data.Text as T; T.pack value` could resolve
across files, then passed after the Haskell resolver bound parser-emitted
qualified import aliases to the prescan `imports_map` module path and resolved
the call as `import_binding`. The same focused gate failed before duplicate
qualified aliases such as `import qualified Data.Text as T` plus
`import qualified Other.Text as T` left `T.pack` unresolved, then passed after
the resolver gathered all matching imported modules and required exactly one
owner before materializing a CALLS row. `go test ./internal/resolutionparity
-run TestGoldenCallGraphCorrectnessHarness/haskell_import_binding -count=1`
failed before the source-backed fixture emitted the expected edge, then passed
with the exact `Data.Text.pack` target while a same-name `Other.Text.pack`
decoy stayed unselected.

No-Observability-Change: Haskell qualified import resolution and duplicate-alias
ambiguity handling are in-memory resolver branches over existing parser import
rows, repository prescan import maps, and the existing code entity index. They
add no graph query, graph write shape, queue table, worker, lease, batch
setting, runtime knob, metric instrument, metric label, span, route, or log key.
Operators still diagnose code-call extraction through the existing `code call
materialization completed` log fields and reducer execution spans/counters.
