# AGENTS.md - coordinator/reachability guidance

## Read first

1. `README.md`
2. `planner.go`
3. `go/internal/coordinator/activation_go_modules.go` for the root seam

## Invariants

- Keep this package free of stores, clocks, and network calls. The root passes the
  page in.
- Do not import `internal/coordinator`.
- Never put a remote URL in a run id, scope id, work-item id, generation id, or
  fairness key. `TestPlanNeverPutsARemoteURLInAnIdentifier` guards this.
- Skip reasons are log keys. Keep the set closed and free of repository values.
- Keep identities deterministic for the same instance, plan key, scope, and commit.
