# code

Documentation namespace for the reducer's source-code analysis families
(issue #6061, step 3 of `docs/internal/design/reducer-target-tree.md`). This
directory owns no runtime behavior. Each child is its own Go package:

| Child | Package | Owns |
| --- | --- | --- |
| `call/` | `call` (imported as `codecall`) | Code-call extraction, the explicit language resolver table, and code-call shared-intent rows. Children: `shared/` (the code-entity index and resolver contract), one leaf per resolver language, `materialization/` (the code-call materialization handler plus the handles_route, runs_in, and invokes_cloud_action families), and `projection/` (the code-call projection runner) |
| `taint/` | `taint` | Code taint and interprocedural evidence materialization and projected-edge backfills |
| `value/` | `value` | Value-flow fixpoint program assembly, component cache, cloud-sink loading, backfill state marker. Child `cleanup/` runs the generation-scoped value-flow stale-evidence sweep |
| `shell/` | `shell` | Shell-exec fact extraction, materialization, and shared-intent row construction for `Function-[:EXECUTES_SHELL]->ShellCommand` |
| `function/` | `function` (documentation namespace; see `code/function/README.md`) | Parents `function/summary/`, the durable value-flow function-summary persistence family |
| `semantic/` | `semantic` | Turns `content_entity` facts into canonical semantic-entity graph nodes (Annotation, Typedef, Component, Function, and related node kinds) |
| `owners/` | `owners` | Projects `Repository-[:DECLARES_CODEOWNER]->CodeownerTeam` edges from `codeowners.ownership` facts |

`codeintel` (code-root reachability projection) is planned for `intel/` and
still lives at `go/internal/reducer/codeintel` until its relocation lands.

## What stays in the reducer root

- `code_import_*` (6): the code-import repository-edge family, planned for
  `repodependency/import`, not here. Each file carries a justified
  `//nolint:dirgate` marker because its name collides with this `code/`
  subpackage under the dirgate naming rule.

The code-call materialization handler, the code-call projection runner, and
the value-flow stale cleanup runner used to stay in the root too. They moved
to `call/materialization/`, `call/projection/`, and `value/cleanup/` under
issue #6061, and the root keeps their external `reducer.X` spellings through
the compat buckets.
