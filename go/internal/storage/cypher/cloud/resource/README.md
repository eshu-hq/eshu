# Cloud resource writers

Shared CloudResource graph writers: the node writer
(`node_writer.go`, including the `ifadeterminismteeth`
build-tag split), the shared edge writer
(`edge_writer.go`), and the CloudResource retract dispatch
the provider leaves anchor on. Cloud-wide cross-cutting dispatch, registry,
search-key, and retry-classification tests that span several cloud leaves
also live here, because the root package cannot import this leaf.

## Consumers

- `go/cmd/reducer` constructs `resource.CloudResourceNodeWriter` and
  `resource.CloudResourceEdgeWriter` directly.
- Provider leaves anchor their retract dispatch on the shared
  CloudResource edge contract.
