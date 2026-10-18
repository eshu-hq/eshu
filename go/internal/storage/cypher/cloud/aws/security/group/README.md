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

## Proof

No-Regression Evidence: pure package move from
`go/internal/storage/cypher` (issue #6694 cloud slice). Every Cypher
template is byte-identical to the reviewed base, and the hermetic suite
passes in this leaf. The executed Cypher, batching, and transaction shape
are unchanged. The registry reason strings now cite the writer's leaf path
(the registry's own citation guard requires resolvable citations).

No-Observability-Change: no metric, span, log, or status field is added or
changed by this move.
