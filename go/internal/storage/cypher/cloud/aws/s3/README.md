# Cloud AWS S3 writers

AWS S3 graph writers: external-principal grants
(`external_principal_grant_writer.go`), internet-exposure posture nodes
(`internet_exposure_node_writer.go`), and the logs-to edge
(`logs_to_edge_writer.go`).

## Consumers

- `go/cmd/reducer` constructs the S3 writers directly.
- The `edge/materialized` registry reads the logs-to retract and identity
  constants directly.
