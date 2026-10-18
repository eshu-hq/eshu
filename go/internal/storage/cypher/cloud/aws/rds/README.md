# Cloud AWS RDS writer

AWS RDS posture node writer (`posture_node_writer.go`), persisting
database posture (exposure, encryption, backup) onto the shared
CloudResource nodes without ever fabricating them.

## Consumers

- `go/cmd/reducer` constructs `rds.RDSPostureNodeWriter` directly.

## Proof

No-Regression Evidence: pure package move from
`go/internal/storage/cypher` (issue #6694 cloud slice). Every Cypher
template is byte-identical to the reviewed base, and the hermetic suite
passes in this leaf. The executed Cypher, batching, transaction shape, and
the existence-read-before-MERGE posture contract are unchanged.

No-Observability-Change: no metric, span, log, or status field is added or
changed by this move.
