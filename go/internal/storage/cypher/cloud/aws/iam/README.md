# Cloud AWS IAM writers

AWS IAM edge writers (`can_assume_edge_writer.go`,
`can_perform_edge_writer.go`, `escalation_edge_writer.go`,
`instance_profile_role_edge_writer.go`) and the secrets/IAM graph
writer (`secrets_graph_writer.go`) for vault-backed identity posture.

## Consumers

- `go/cmd/reducer` constructs the IAM writers directly.
- The `edge/materialized` registry reads the IAM retract and identity
  constants directly.

## Proof

No-Regression Evidence: pure package move from
`go/internal/storage/cypher` (issue #6694 cloud slice). Every Cypher
template is byte-identical to the reviewed base, and the hermetic suite
passes in this leaf. The executed Cypher, batching, and transaction shape
are unchanged.

No-Observability-Change: no metric, span, log, or status field is added or
changed by this move.
