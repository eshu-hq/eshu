# Cloud GCP relationship writer

GCP cloud-resource relationship edge writer
(`gcp_cloud_resource_edge_writer.go`), materializing GCP resource hierarchy
evidence as edges between CloudResource nodes.

## Consumers

- `go/cmd/reducer` constructs
  `relationship.GCPCloudResourceEdgeWriter` directly.
