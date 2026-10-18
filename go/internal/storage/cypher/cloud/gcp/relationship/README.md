# Cloud GCP relationship writer

GCP cloud-resource relationship edge writer
(`gcp_cloud_resource_edge_writer.go`), materializing GCP resource hierarchy
evidence as edges between CloudResource nodes.

## Consumers

- `go/cmd/reducer` constructs
  `relationship.GCPCloudResourceEdgeWriter` directly.

## Proof

No-Regression Evidence: pure package move from
`go/internal/storage/cypher` (issue #6694 cloud slice). Every Cypher
template is byte-identical to the reviewed base, and the hermetic suite
passes in this leaf. The executed Cypher, batching, and transaction shape
are unchanged.

No-Observability-Change: no metric, span, log, or status field is added or
changed by this move.
