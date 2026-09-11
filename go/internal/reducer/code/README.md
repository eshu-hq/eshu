# code

Documentation namespace for the reducer's source-code analysis families
(issue #6061, step 3 of `docs/internal/design/reducer-target-tree.md`). This
directory owns no runtime behavior. Each child is its own Go package:

| Child | Package | Owns |
| --- | --- | --- |
| `call/` | `call` (imported as `codecall`) | Code-call extraction, per-language resolvers, the code-entity index, and code-call shared-intent rows |
| `taint/` | `codetaint` | Code taint and interprocedural evidence materialization and projected-edge backfills |
| `value/` | `valueflow` | Value-flow fixpoint program assembly, component cache, cloud-sink loading, backfill state marker |
| `shell/` | `shell` | Shell-exec fact extraction, materialization, and shared-intent row construction for `Function-[:EXECUTES_SHELL]->ShellCommand` |
| `function/` | `function` (documentation namespace; see `code/function/README.md`) | Parents `function/summary/`, the durable value-flow function-summary persistence family |

`codeintel` (code-root reachability projection) is planned for `intel/` and
still lives at `go/internal/reducer/codeintel` until its relocation lands.

## What stays in the reducer root

- `code_call_materialization.go`: `CodeCallMaterializationHandler`. It
  composes `codecall` rows with the handles_route, runs_in, and
  invokes_cloud_action families that live in root, so it cannot sit below
  them.
- `code_call_projection_*.go` (7): the code-call projection runner. It needs
  the root lease and shared-projection machinery that `sharedintent/doc.go`
  pins to root.
- `code_value_flow_stale_cleanup_runner.go`: a side runner that needs the
  root `PartitionLeaseManager` and `Service.startSideRunners` wiring.
- `code_import_*` (6): planned for `repodependency/import`, not here.

Each root stayer carries a justified `//nolint:dirgate` marker because its
name collides with this `code/` subpackage under the dirgate naming rule.
