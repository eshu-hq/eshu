# Cloud AWS S3 writers

AWS S3 graph writers: external-principal grants
(`external_principal_grant_writer.go`), internet-exposure posture nodes
(`internet_exposure_node_writer.go`), and the logs-to edge
(`logs_to_edge_writer.go`).

## Consumers

- `go/cmd/reducer` constructs the S3 writers directly.
- The `edge/materialized` registry reads the logs-to retract and identity
  constants directly.

## Proof

No-Regression Evidence: pure package move from
`go/internal/storage/cypher` (issue #6694 cloud slice). Every Cypher
template is byte-identical to the reviewed base, and the hermetic suite
passes in this leaf. The executed Cypher, batching, transaction shape, and
the existence-read-before-MERGE posture contract are unchanged.

No-Observability-Change: no metric, span, log, or status field is added or
changed by this move.
