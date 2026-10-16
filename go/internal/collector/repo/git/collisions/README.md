# collisions

Basename-collision diagnostics extracted from the git collector
(`selection_discovery.go`) to keep that file under the repo's 500-line
cap. No behavior change: same heuristic, same log line, same counter.

## Layout

- `doc.go` — the godoc contract.
- `collisions.go` — `ReportRepositoryBasenameCollisions`.
- `collisions_test.go` — unit tests (external test package); the
  selector-level gating tests stay in package `git`.

## Signal

When the same repository basename appears at more than one distinct path,
a `repository basename collision detected (possible accidental corpus
nesting)` WARN log names the basename with `path_count`,
`surplus_count`, a bounded `path_sample` (and `total_path_count` when
truncated), and the `eshu_dp_repository_basename_collision_total` counter
advances by the surplus count. No path or basename labels ride on the
metric; those details stay in the structured log.
