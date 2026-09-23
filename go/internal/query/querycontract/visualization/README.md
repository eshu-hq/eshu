# querycontract/visualization

Visualization-packet contract shared by the query handler families: the bounded
node/edge subgraph a handler returns so a client can draw a graph view without
running its own traversal.

## What is here

| file | holds |
| --- | --- |
| `packet.go` | `VisualizationPacket`, `VisualizationNode`, `VisualizationEdge`, `VisualizationLimits`, `VisualizationTruncation`, `VisualizationView` and its constants, `VisualizationMaxNodes`/`VisualizationMaxEdges`, `VisualizationBuilder` (`NewVisualizationBuilder`, `SetTruth`, `AddNode`, `AddEdge`, `Empty`, `EdgeCount`, `Finalize`), `UnsupportedVisualizationPacket`, and the `VisualizationNodeID`/`VisualizationEdgeID` hashers |
| `packet_merge.go` | unexported node-merge rules the builder applies when two observations share a node ID: role priority, category, evidence-handle dedup and the stronger truth label |

The builder is deterministic: `Finalize` sorts nodes and edges, enforces the
node and edge caps, and records what it dropped in `Truncation`, so the same
input rows always produce the same packet.

## Dependencies

Inbound: four non-test files and one test file import this package --
`query/visualization` (the derivation route), `codequery/visualization`
(graph-query packets), `codequery/aliases.go`, and
`service/story_evidence_graph.go`.

Outbound: the parent `querycontract` for `TruthEnvelope` and the
`TruthLevel*` constants, and `querycontract/evidence` for
`EvidenceCitationHandle`. The parent must not import this package, or the
import becomes a cycle.

## Telemetry

None. The package builds values in memory and performs no I/O; the handlers
that call it own their spans and metrics.
