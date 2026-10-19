# Phase group leaf guidance

## Ownership

- This package owns statement-parameter sanitization, the all-retract phase
  detector, profiled sequential execution, and the file-statement diagnostic
  allowlist.
- The parent `cypher` package owns the `Statement` and `Operation` types,
  the `StatementMetadata*`/`CanonicalPhase*` vocabulary
  (`phase_group_metadata.go`, deliberately not moved), and every backend
  executor. This leaf imports the parent for those (Pattern B,
  `edge/writer` precedent).
- The parent and white-box parent tests must NEVER import this package
  (import cycle). Backend executors (`cmd/ingester`, `cmd/bootstrap-index`,
  `storage/nornicdb`, the offline replay tier) and the `edge/writer` leaf
  import it.

## Invariants

- Every backend executor that sends a `Statement` to a driver MUST route its
  parameters through `SanitizeStatementParameters` first: unreferenced
  `_eshu_*` parameters are wasteful and, on NornicDB, can make a grouped
  `DETACH DELETE` silently no-op.
- Keep the no-alloc fast path: when params carry no diagnostic key the input
  map is returned unchanged, so callers must treat the result as read-only.
- `CanonicalFileStatementProfile` stays a closed allowlist over the
  `canonical/node` file templates: unknown statements are excluded from the
  opt-in files-phase probe, and query text plus row values must never be
  copied into diagnostic events. When the writer gains a file template, add
  its profile here — `TestCanonicalFileWriterStatementsAllHaveProfiles`
  fails until then.
- Keep the package clause as `package group`; external callers use the
  `phasegroup` alias (`group` alone is too generic and collides with local
  variables at call sites).
- Never import the parent-test fakes from here.
