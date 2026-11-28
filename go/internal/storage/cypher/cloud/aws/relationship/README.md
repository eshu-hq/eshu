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
