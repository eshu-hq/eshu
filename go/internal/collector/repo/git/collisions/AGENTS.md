# AGENTS.md — go/internal/collector/repo/git/collisions

## Read first

1. `go/internal/collector/repo/git/collisions/README.md` — the signal
2. `go/internal/collector/repo/git/AGENTS.md` — the `git -> leaf`
   import direction

## Rules

- This package is a leaf. `git` imports it; it must never import `git`.
- It reports only. Do not add selection, indexing, deletion, or any
  decision that changes which repositories are indexed. The signal is a
  heuristic for accidental corpus nesting, not repository identity.
- Keep the log message, field names, and the
  `eshu_dp_repository_basename_collision_total` counter name stable:
  dashboards read them.
