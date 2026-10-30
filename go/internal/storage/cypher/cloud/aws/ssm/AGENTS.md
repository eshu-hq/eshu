# Cloud AWS SSM package guidance

## Ownership

- This package owns the SSM host inventory writers: observation nodes,
  pairing edges, the committed-capture read, and supersession retracts.
- The parent `cypher` package owns the shared `Executor`, `Statement`,
  `Operation`, `DefaultBatchSize`, retry classification, and the
  posture-existence reader contract. This leaf imports the parent as
  `sourcecypher` for those contracts only; the parent never imports this
  leaf.
- `go/cmd/reducer` constructs these writers directly and adapts them onto
  the reducer's `HostGraphWriter` interface.

## Invariants

- Keep every Cypher template anchored by uid (`MERGE` for writes, indexed
  `MATCH` for reads and retracts). Time comparisons use `*_unix` ints, never
  raw RFC3339 strings.
- The closed `REPORTED_WITH` vocabulary lives here as a literal mirroring
  `edgetype.ReportedWith`. Never interpolate a row-carried relationship
  type.
- `recordingExecutor`, `recordingGroupExecutor`, and `stubCommittedReader`
  in `harness_test.go` are leaf-local copies. Test fakes cannot be imported
  across the package split; keep the copies behavior-identical.
- Never `CREATE` nodes or edges unconditionally; never retract outside one
  host plus the evidence source.
