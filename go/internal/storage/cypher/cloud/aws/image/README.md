# Cloud AWS image writer

AWS Lambda container-image edge writer
(`container_edge_writer.go`), linking CloudResource
nodes to the container images their workloads run.

## Consumers

- `go/cmd/reducer` constructs
  `image.CloudResourceContainerImageEdgeWriter` directly.

## Proof

No-Regression Evidence: pure package move from
`go/internal/storage/cypher` (issue #6694 cloud slice). Every Cypher
template is byte-identical to the reviewed base, and the hermetic suite
passes in this leaf. The executed Cypher, batching, and transaction shape
are unchanged.

No-Observability-Change: no metric, span, log, or status field is added or
changed by this move.
