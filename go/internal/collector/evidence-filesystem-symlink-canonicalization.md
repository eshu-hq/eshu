# Evidence: filesystem repo-path symlink canonicalization (#3874)

Scope: `buildSelectedRepositories` (`git_selection_native.go`) now resolves the
selected repository root through `filepath.EvalSymlinks` in filesystem source
mode so it shares a prefix with the symlink-resolved file paths content discovery
produces (`normalizeScanRoot` already `EvalSymlinks` the scan root).

## Performance

- No-Regression Evidence: this is a correctness fix to path derivation, not a
  hot-path query, graph write, worker/lease, batching, or concurrency change. It
  adds exactly one `filepath.EvalSymlinks` call (one `lstat`-style syscall) per
  selected repository, once per sync cycle, on the repo root — not per file and
  not in any reduce/projection loop.
- Baseline: B-7 golden-corpus gate before the fix — full pipeline drain + graph
  assertions, elapsed 35s (NornicDB, 10-repo corpus).
- After: same gate with the fix — elapsed 35s (unchanged, well under the 1800s
  ceiling); the only behavioral delta is that the canonical directory chain now
  roots at the Repository node, so `Directory`/`File`/`Function` nodes
  materialize (Function=120, File=50, Directory=2) where they previously
  produced 0 on symlinked roots.
- Backend/version: NornicDB (compose default), Postgres 16, filesystem source
  mode. Input shape: bounded — one EvalSymlinks per repo in the selected batch.
- Why safe: the resolve is best-effort (`EvalSymlinks` error keeps the
  unresolved path), scoped to filesystem mode so git mode's
  `repoIDFromManagedPath` identity derivation (which keys off the raw managed
  `ReposDir` prefix) is unchanged, and is covered by a cross-platform regression
  test (`TestNativeRepositorySelectorResolvesSymlinkedRepoPath`).

## Observability

- No-Observability-Change: no metrics, spans, or log keys are added or modified.
  Existing collector selection telemetry already covers the selection path; this
  change only canonicalizes a path string the selector already emitted.
