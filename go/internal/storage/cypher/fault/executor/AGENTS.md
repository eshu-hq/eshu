# AGENTS.md — storage/cypher/fault/executor guidance for LLM assistants

## Read first

1. `go/internal/storage/cypher/fault/executor/doc.go` — what this package
   owns and where it is wired
2. `go/internal/storage/cypher/fault/executor/fault.go` — `FaultingExecutor`,
   the lane behavior, and the capability-passthrough contract
3. `go/internal/storage/cypher/writer.go` — `Executor`, `Statement`,
   `GroupExecutor`, `PhaseGroupExecutor`, `ProbeExecutor`; the contract this
   package decorates but never redefines
4. `go/internal/replay/faultreplay/` — the fault vocabulary before adding a
   fault kind

## Invariants this package enforces

- **Build-tag split is load-bearing.** `fault.go` and `marker.go` carry
  `//go:build ifafaultinjection`; `off.go` carries
  `//go:build !ifafaultinjection`. Both constructors are named
  `NewFaultingExecutor` with identical signatures on purpose. Never merge
  them into one file — the default binary must not link `FaultingExecutor`.
- **Fail closed on missing capabilities.** `ExecuteGroup`,
  `ExecutePhaseGroup`, and `ExecuteProbe` return dedicated errors when the
  inner executor lacks the surface; `NewFaultingExecutor` rejects ambiguous
  scripts and a restart fault without a sentinel path.
- **Every wrapper that forwards `GroupExecutor` must forward `ProbeExecutor`
  identically** (parent-package contract in `cypher/doc.go`). This decorator
  forwards both and injects faults only on the mutating seams.
- **No Cypher authorship.** Matching reads `Statement.Cypher` text; nothing
  here builds statements. Keep it that way — statement builders belong with
  their writers, not in a fault decorator.
- **One-way dependency.** This package imports the parent `cypher` package
  for the seam types. The parent must never import this package.

## Proof before touching the tag split

`scripts/test-verify-ifa-fault-injection.sh` asserts both files exist with
their tags, and the CI triggers in `specs/ci-gates.v1.yaml` plus
`.github/workflows/ifa-determinism-gate.yml` name this directory. Move a
file here only with those gates repointed in the same change.
