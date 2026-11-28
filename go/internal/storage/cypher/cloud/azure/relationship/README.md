# Cloud Azure relationship writer

Azure cloud-resource relationship edge writer
(`azure_cloud_resource_edge_writer.go`), materializing Azure resource
hierarchy evidence as edges between CloudResource nodes.

## Consumers

- `go/cmd/reducer` constructs
  `relationship.AzureCloudResourceEdgeWriter` directly.
