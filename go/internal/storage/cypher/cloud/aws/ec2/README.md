# Cloud AWS EC2 writers

AWS EC2 graph writers: instance nodes (`instance_node_writer.go`),
instance identity (`instance_identity_node_writer.go`), internet
exposure (`internet_exposure_node_writer.go`), block-device KMS posture
(`block_device_kms_posture_node_writer.go`), and the uses-profile edge
(`uses_profile_edge_writer.go`). The posture live test lives here
because most of its cases are EC2 writers.

## Consumers

- `go/cmd/reducer` constructs the EC2 writers directly.
- The `edge/materialized` registry reads the uses-profile retract and
  identity constants directly.

## Proof

No-Regression Evidence: pure package move from
`go/internal/storage/cypher` (issue #6694 cloud slice). Every Cypher
template is byte-identical to the reviewed base, and the hermetic suite
passes in this leaf. The executed Cypher, batching, transaction shape, and
the existence-read-before-MERGE posture contract are unchanged.

No-Observability-Change: no metric, span, log, or status field is added or
changed by this move.
