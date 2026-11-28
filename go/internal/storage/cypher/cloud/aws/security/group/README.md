# Cloud AWS security-group writers

AWS security-group graph writers: endpoint nodes
(`security_cidr_node_writer.go`) and the reachability edge writer
(`security_reachability_edge_writer.go`) with its projected-source
ledger (`security_reachability_edge_writer_ledger.go`) for
UID-anchored retracts.

## Consumers

- `go/cmd/reducer` constructs the security-group writers directly.
- The `edge/materialized` registry reads the reachability retract and
  identity constants directly, never a copy of their text.
