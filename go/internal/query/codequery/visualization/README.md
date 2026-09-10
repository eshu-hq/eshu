# visualization

Graph-query result projection for the code family (`codequery`).

## What lives here

- `packet.go` — `BuildGraphQueryVisualizationPacket`: projects graph
  nodes, relationships, and paths in executed read-only Cypher rows
  into a bounded packet. Scalar-only rows yield an explicit
  unsupported packet.

## What stays in `codequery`

`codequery/cypher.go` owns the visualize-query handler: cypher
validation, the bounded read, truncation, and the response envelope.
It calls this leaf for the projection.

## Invariants

- Pure transformation: no graph access beyond the rows handed in.
- Node IDs derive from graph element ids, so the same node always
  yields the same packet node ID regardless of row order.
- Edges survive only when both endpoints appear as nodes; dangling
  edges land in the truncation block, never inventing nodes.
- This package never imports `codequery` or root `query`.
