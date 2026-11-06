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
- `StaleEvidence` requires evidence, skew, and skew age together. Measure skew age from
  `EvidenceLeftAt`, never from the evidence's activation: an `Unchanged` claim commits
  nothing, so a quiet repository's evidence is old by design.
- The bound is two rotation periods, `slotCount x interval` from the same page read, not
  two intervals. The multiplier is fixed by the eshu-hq/eshu#10055 ruling. Do not make it
  configurable.
- A zero `EvidenceLeftAt` with skew means retention pruned the history: fence and count it.
  Never fence a generation with no commit: count it instead.
