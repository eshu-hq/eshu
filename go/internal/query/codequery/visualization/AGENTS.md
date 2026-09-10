# AGENTS.md — codequery/visualization

Bonded leaf of the code family. The projector is deterministic by
contract; the telemetry-coverage-discipline skill governs observability
questions, cypher-query-rigor any read-shape change upstream.

## Ownership

- `packet.go` owns the row projection. Column iteration is sorted so
  map order never leaks into the packet; node/edge finalize sorts by
  ID.
- `codequery/cypher.go` owns the handler: validation, the bounded
  read, truncation, and the envelope. Do not move the handler here.

## Rules for agents

- Never import `codequery` or root `query` from this package.
- New files need a useful Go doc comment; no placeholders.
- The Neo4j driver import is depguard-excepted for projection types
  only (see `go/.golangci.yml`): never open a session from this
  package.
- Scalar rows stay unsupported: fabricating a subgraph from
  properties is a correctness bug, not a feature gap.
