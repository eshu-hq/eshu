# Cloud AWS image writer

AWS Lambda container-image edge writer
(`container_edge_writer.go`), linking CloudResource
nodes to the container images their workloads run.

## Consumers

- `go/cmd/reducer` constructs
  `image.CloudResourceContainerImageEdgeWriter` directly.
