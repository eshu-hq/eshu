# AGENTS.md — internal/storage/postgres/consumption

Scoped instructions for this package. The root `AGENTS.md` and the parent
`internal/storage/postgres/AGENTS.md` still apply.

- Never hold the scope row across more than one page. A single
  scope-spanning transaction is the #7663 regression.
  `TestPackageManifestConsumptionScopeRebuildReleasesScopeBetweenPagesLive`
  in the parent package fails when pages stop committing separately.
- Keep `FOR NO KEY UPDATE` on the scope row. `FOR UPDATE` blocks every
  foreign-key insert that references the scope. A weaker mode (`FOR SHARE`,
  `FOR KEY SHARE`) no longer excludes the ingester's sidecar writes. If a new
  writer sets `eshu.package_manifest_consumption_keys_writer`, it must take
  this row lock first, or it must not set the GUC.
- Keep the dirty-row fence and the `xmin` compare-and-delete. Do not compare
  on `marked_at`.
  `TestPackageManifestConsumptionScopeRebuildKeepsOldWriterDirtyLive` fails
  when the last page deletes the dirty row unconditionally.
- Every transaction sets `lock_timeout`. A 55P03 must return an error, never
  a skipped scope: the initial cursor must not advance past a scope that did
  not finish.
- Do not add scope-leading indexes on the sidecar tables here; #7420 owns them.
- This package imports `facts`, `telemetry`, `postgres/db`, and
  `postgres/coordination`, never the parent `postgres` package. The parent
  supplies the page query and the refresh through `PageLoader` and `Refresher`.
- `SBTRepair` must stay additive: no delete, no `DO UPDATE`, no dirty row, and no
  writer setting. It keeps the scope row lock per page, which is what stops it
  inserting a key from a payload an ingester is rewriting. Write its marker only
  after a walk that inserted nothing.
