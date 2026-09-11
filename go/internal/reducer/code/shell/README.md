# internal/reducer/code/shell

## Purpose

Reduces parser command-call evidence into durable shared-projection intents
for `Function-[:EXECUTES_SHELL]->ShellCommand` (issue #6061). It records
command-construction presence only — never raw command text or arguments.

`ExecMaterializationHandler.Handle` loads `repository`/`file` facts, extracts
canonical edge rows through `ExtractExecRows`, and reuses the SQL-relationship
family's delta scope and repo-ID merge (`sqlrelationship.BuildDeltaScope`,
`sqlrelationship.MergeRepositoryIDs`) rather than duplicating them: both
families derive the same per-repository `delta_generation`/
`delta_relative_paths` shape from the same `repository` facts.

`BuildSharedIntentRows` and `BuildRefreshIntents` build the durable per-edge
and per-repo-refresh intent rows, matching the `sqlrelationship` and
`inheritance` sibling emitter shape.

## Ownership boundary

**Owns:** shell-exec fact extraction (`ExtractExecRows`), materialization
(`ExecMaterializationHandler`), and shared-intent row construction
(`BuildSharedIntentRows`, `BuildRefreshIntents`).

**Does not own:** the SQL-relationship delta scope this package reuses
(`sqlrelationship`), the code-call `PayloadInt` numeric-coercion helper
(`code/call`), or the shared-projection worker/runner machinery that consumes
the intents this package emits (reducer root).

## Exported surface

| symbol | what it is |
|---|---|
| `ExecMaterializationHandler` / `ExecIntentWriter` | the domain handler and the durable-intent write port it requires |
| `ExtractExecRows` | pure extraction: parser file facts -> canonical shell-exec edge rows |
| `LoadMaterializationFacts` / `MaterializationFactKinds` | the scoped fact-kind loader for `repository`/`file`, and the kind set it requests |
| `BuildSharedIntentRows` / `BuildRefreshIntents` | the per-edge and per-repo-refresh shared-projection intent builders |

The reducer root wires `ExecMaterializationHandler` in
`defaults_domain_catalog.go` and declares the `ShellExecIntentWriter` field on
`DefaultHandlers` (`defaults.go`), keeping the `reducer.ShellExecIntentWriter`/
`reducer.ShellExecMaterializationHandler` spellings through the shell-exec
stanza of `compat_projection.go`. The reducer root's cross-domain sibling
proofs (`sibling_edge_intent_delta_gate_test.go`,
`sibling_edge_intent_retract_reachability_test.go`) drive
`BuildRefreshIntents`/`BuildSharedIntentRows` directly, alongside
`inheritance` and `sqlrelationship`'s equivalents, through the
`buildShellExecRefreshIntents`/`buildShellExecSharedIntentRows` forwarders.

## Dependencies

`internal/facts` (`Envelope`), `internal/reducer/contract` (`Intent`,
`Result`, `DomainShellExec`, `DomainShellExecMaterialization`),
`internal/reducer/factload` (`FactLoader`, `LoadFactsForKinds`,
`FactKindRepository`, `FactKindFile`), `internal/reducer/schemadecode`
(`DecodeCodegraphRepository`, `DecodeCodegraphFile`,
`BuildProjectionContexts`), `internal/reducer/sharedintent` (`Row`, `Input`,
`Build`, `ProjectionContext`, the repo-refresh/retract-fence vocabulary),
`internal/reducer/payloadcore` (payload accessors), `internal/reducer/
sqlrelationship` (the shared delta-scope builder and the embedded-SQL
function-id index the parser also populates for embedded shell commands), and
`internal/reducer/code/call` (`PayloadInt`). No dependency on the reducer
root.

## Telemetry

No dedicated metric instrument. `Handle` emits two structured logs, "shell
exec materialization started" and "shell exec materialization completed",
with `scope_id`, `generation_id`, `domain` (started only), and
`intent_count`/`edge_count`/`repo_count` (completed only). Shared-projection
writes and retracts go through the generic worker/EdgeWriter the reducer root
wires, which is unchanged by this move.

## Gotchas / invariants

- **The extracted edge target never encodes command text.** `ExtractExecRows`
  hashes `(repoID, sourcePath, functionEntityID, lineNumber, api)` into the
  `shell-command:` target id; do not add raw command arguments to that
  identity or the row payload.
- **Delta scoping is shared with `sqlrelationship`, not reimplemented.** A
  divergent shell-exec delta scope would silently disagree with the
  SQL-relationship family's over the same `repository` facts.
- **`ProjectionDomain` is `contract.DomainShellExec`, a plain string
  constant, not `contract.Domain`.** `sharedintent.Input.ProjectionDomain` is
  typed `string`, matching every other family's emitter.

## Related docs

- `go/internal/reducer/README.md` — the root package and its subpackage inventory
- `go/internal/reducer/sqlrelationship/README.md` — the delta-scope/repo-ID-merge sibling this package reuses
- `go/internal/reducer/code/README.md` — the `code/` namespace parent
- `docs/internal/design/reducer-target-tree.md` — the #6061 restructure

No-Regression Evidence: #6061 moves shell-exec fact extraction,
materialization, and shared-intent row construction out of the reducer root
into this new package, without changing any field, exported behavior, wire
string, or call order. `ShellExec*` exported identifiers dropped the `Shell`
prefix per `docs/internal/naming.md` (`ExtractShellExecRows` ->
`ExtractExecRows`, `ShellExecIntentWriter` -> `ExecIntentWriter`,
`ShellExecMaterializationHandler` -> `ExecMaterializationHandler`); the
reducer root keeps every one of these spellings through the shell-exec stanza
of `compat_projection.go`, so no external caller needed a source change.
Measured from `go/`, with `GOROOT` unset: `go build ./...`, `go vet
./internal/reducer/... ./cmd/reducer/... ./internal/storage/...`, and `go
test ./internal/reducer/... ./cmd/reducer/... ./internal/storage/cypher/...
./internal/replay/... -count=1` all exited 0 on this branch. `git diff
--check` exited 0.

No-Observability-Change: this move adds no queue domain, worker, lease,
graph, or Postgres operation, runtime setting, metric instrument, metric
label, span, or status surface. The two structured-log messages and their
fields listed under Telemetry above are unchanged; only the package that owns
the code moved.
