# Agent instructions: internal/reducer/code/shell

Scoped rules for this directory. The root `AGENTS.md` still applies.

## What this package is

Reduces parser command-call evidence into durable shared-projection intents
for `Function-[:EXECUTES_SHELL]->ShellCommand` (issue #6061). Moved out of
the reducer root as its own package. See the README's Purpose and Ownership
boundary sections for what this package reuses from `sqlrelationship` and
`code/call` rather than owning itself.

## Read first

- Repository-root `AGENTS.md`
- `go/internal/reducer/AGENTS.md`
- `go/internal/reducer/code/shell/README.md`
- `go/internal/reducer/sqlrelationship/README.md` (the delta-scope sibling this package reuses)
- `docs/internal/design/reducer-target-tree.md`

## Invariants

- **No import of the reducer root, ever.** This package is a leaf below
  `internal/reducer`: the root imports it (via the shell-exec stanza of
  `compat_projection.go` and `defaults_domain_catalog.go`'s wiring), never
  the reverse.
- **Never widen the extracted row's identity to carry raw command text or
  arguments.** `ExtractExecRows` hashes `(repoID, sourcePath,
  functionEntityID, lineNumber, api)` into the `shell-command:` target id
  precisely so command text stays out of the graph.
- **Reuse `sqlrelationship`'s delta scope; do not fork it.** A divergent
  shell-exec delta-scope implementation would silently disagree with the
  SQL-relationship family's over the same `repository` facts.

## Common changes

Adding a new shell-exec edge field: extend the row map `ExtractExecRows`
builds (the `map[string]any` under the `for _, command := range
payloadcore.MapSlice(...)` loop in `handler.go`), then thread it through
`BuildSharedIntentRows`' payload in `intents.go` if the field belongs on the
durable intent too.

## Failure modes to avoid

- Adding a local copy of `sqlrelationship.BuildDeltaScope` or
  `sqlrelationship.EmbeddedSQLFunctionIDsByNameLine` instead of importing
  `sqlrelationship` directly — this package already sits below it in the
  dependency graph, so there is no cycle to work around.
- Exporting a new unexported helper "just in case" a future caller needs it.
  `Handler`, `IntentWriter`, `ExtractExecRows`,
  `LoadMaterializationFacts`, `BuildSharedIntentRows`, and
  `BuildRefreshIntents` are exported because the reducer root's compat
  forwarders and cross-domain sibling test tables need them; nothing else in
  this package has an external caller today.

## Do not change without ADR review

- The `shell-exec:v1` partition-key version prefix
  (`partitionKeyVersion` in `intents.go`) — changing it changes which
  partition an in-flight intent lands in.
