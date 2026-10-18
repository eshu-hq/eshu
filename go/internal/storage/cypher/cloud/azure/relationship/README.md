# Cloud Azure relationship writer

Azure cloud-resource relationship edge writer
(`azure_cloud_resource_edge_writer.go`), materializing Azure resource
hierarchy evidence as edges between CloudResource nodes.

## Consumers

- `go/cmd/reducer` constructs
  `relationship.AzureCloudResourceEdgeWriter` directly.

## Proof

No-Regression Evidence: pure package move from
`go/internal/storage/cypher` (issue #6694 cloud slice). Every Cypher
template is byte-identical to the reviewed base, and the hermetic suite
passes in this leaf. The executed Cypher, batching, and transaction shape
are unchanged.

No-Observability-Change: no metric, span, log, or status field is added or
changed by this move.
