# AGENTS.md — internal/workflow/parity guidance for LLM assistants

## Read first

1. `go/internal/workflow/parity/README.md` — ownership, legs, and operation
2. `go/internal/workflow/parity/doc.go` — godoc contract and non-negotiables
3. `go/internal/workflow/parity/backend.go` — `Backend`, `Classify`, both legs
4. `go/internal/workflow/parity/conformance_test.go` — suite runner and slices

## Invariants this package enforces

- **Two isolated databases, never shared tables** — the in-tree leg uses
  one `postgresproof` schema per run; the satellite leg truncates only
  satellite-owned tables. A scenario that needs cross-leg reads filters
  by its deterministic run prefix.
- **Every scenario drains what it seeds** — `drainScenario` asserts the
  queue is idle at scenario end. `ClaimNext` selects globally, so an
  undrained scenario contaminates the next one.
- **Only codes and durable values compare** — message text is recorded
  for diagnosis but never compared. A known divergence needs a
  scenario-specific comparator pinning the exact pair plus a tracking
  issue (see `compareDuplicateReplay` and #9428); never loosen `Equal`.
- **Satellite leg runs dark or local only** — local flip container or
  a dark deployment with synthetic IDs plus cleanup. Never point the suite at
  an authoritative deployment and never replay live claims.
- **Fixed clock and deterministic IDs** — no wall-clock reads or random
  IDs inside scenario bodies; extend `Suite`, never ad-hoc fixtures.
- **Throughput asserts function, not ratio, locally** — the local
  satellite leg may run emulated; the binding ratio comparison belongs
  to the native dark-deployment run.
