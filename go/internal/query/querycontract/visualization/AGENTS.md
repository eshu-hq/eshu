# Agent instructions: querycontract/visualization

Contract types and a pure, deterministic builder. No reader, no I/O, no
telemetry.

- Never import this package from `querycontract`; the parent is imported here
  for `TruthEnvelope`, so the reverse import is a cycle.
- `query/visualization` is a different package with the same name. Alias the
  import only where both meet in one file.
- The exported names keep their `Visualization` prefix from the move out of
  `querycontract` (#6597), as `evidence` kept `EvidenceCitation*`. Renaming
  them is a separate change that must repoint every caller in one PR.
- `Finalize` output order and the node/edge ID hashes are part of the
  response: equal identities must keep hashing to the same ID regardless of
  iteration order. Changing either needs a test pinning the new shape.

Verify with `cd go && go vet ./internal/query/... && go test ./internal/query/... ./internal/queryplan/... -count=1`.
