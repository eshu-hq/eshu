# Agent instructions: internal/reducer/runtime/declaration

Scoped rules for this directory. The root `AGENTS.md` still applies.

## Read first

- `README.md` in this directory.
- `go/internal/reducer/AGENTS.md`.
- `go/internal/storage/cypher/edge/writer/runtime_declaration.go` for the
  Cypher this handler drives.
- `docs/internal/evidence/8816-runtime-declaration-graph.md` for the PROFILE
  numbers behind the schema and retract shapes.

## Invariants

- Never import the reducer root. The root imports this package.
- The MERGE identity is exactly (repo_id, relative_path, line_number, runtime,
  order_index), the key the #8815 arbiter ruling fixed. Do not add a mutable
  field to it.
- A delta generation retracts only its changed and deleted paths; a full
  generation retracts each repository whole. Do not collapse the two.
- `EvidenceSource` is durable: the retracts filter on it. Changing it orphans
  every node already written.
- Nullable version parts stay nil. Never default them to 0.

## Common changes

Adding a field to the node: add it to `runtimedeclarationv1.Version` first
(contract change, `eshu-contract-rigor`), then to `declarationRow` in
`rows.go` and the `SET` list in the writer's upsert Cypher, in one change.
