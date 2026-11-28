# Cloud AWS RDS writer

AWS RDS posture node writer (`posture_node_writer.go`), persisting
database posture (exposure, encryption, backup) onto the shared
CloudResource nodes without ever fabricating them.

## Consumers

- `go/cmd/reducer` constructs `rds.RDSPostureNodeWriter` directly.
