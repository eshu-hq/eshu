# Agent instructions: querycontract/visualization

Contract types and a pure, deterministic builder. No reader, no I/O, no
telemetry.

- Never import this package from `querycontract`; the parent is imported here
  for `TruthEnvelope`, so the reverse import is a cycle.
- `query/visualization` and `codequery/visualization` are different packages
  with the same name, and both import this one as `contractviz` so their
  `contractviz.VisualizationPacket` never reads as a self-reference. Any other
  package named `visualization` that imports this one should do the same.
- The exported names keep their `Visualization` prefix from the move out of
  `querycontract` (#6597), as `evidence` kept `EvidenceCitation*`. Renaming
  them is a separate change that must repoint every caller in one PR.
- `Finalize` output order and the node/edge ID hashes are part of the
  response: equal identities must keep hashing to the same ID regardless of
  iteration order. Changing either needs a test pinning the new shape.

Verify with `cd go && go vet ./internal/query/... && go test ./internal/query/... ./internal/queryplan/... -count=1`.
