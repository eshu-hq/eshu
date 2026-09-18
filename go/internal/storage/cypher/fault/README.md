# storage/cypher/fault

Namespace for the test-only fault-injection executors behind the Cypher
`Executor` seam.

- `executor/` — the `FaultingExecutor` decorator (live under the
  `ifafaultinjection` build tag) plus the no-op stub every other tag links.

This parent carries documentation only. The fault vocabulary (which faults
exist and what they mean) is owned by `go/internal/replay/faultreplay`;
the reducer wiring that installs the decorator is owned by
`go/cmd/reducer` (`ifa_fault_wiring.go`, same build tag). Nothing in the
default production build links through this subtree beyond the stub's
`faultreplay.Script` type in one signature.
