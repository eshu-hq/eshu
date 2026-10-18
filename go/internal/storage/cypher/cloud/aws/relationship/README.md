# Cloud AWS workload-relationship writer

AWS workload-to-cloud relationship edge writer
(`workload_cloud_writer.go`), materializing exact
workload-anchored CloudResource evidence as WorkloadInstance USES
CloudResource edges. It never creates endpoint nodes.

## Consumers

- `go/cmd/reducer` constructs
  `relationship.WorkloadCloudRelationshipWriter` directly.
- The `edge/materialized` registry reads the retract and identity
  constants directly, never a copy of their text.

## Proof

No-Regression Evidence: pure package move from
`go/internal/storage/cypher` (issue #6694 cloud slice). Every Cypher
template is byte-identical to the reviewed base, and the hermetic suite
passes in this leaf. The executed Cypher, batching, and transaction shape
are unchanged.

No-Observability-Change: no metric, span, log, or status field is added or
changed by this move.
