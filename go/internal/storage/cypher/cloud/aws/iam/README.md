# Cloud AWS IAM writers

AWS IAM edge writers (`can_assume_edge_writer.go`,
`can_perform_edge_writer.go`, `escalation_edge_writer.go`,
`instance_profile_role_edge_writer.go`) and the secrets/IAM graph
writer (`secrets_graph_writer.go`) for vault-backed identity posture.

## Consumers

- `go/cmd/reducer` constructs the IAM writers directly.
- The `edge/materialized` registry reads the IAM retract and identity
  constants directly.
