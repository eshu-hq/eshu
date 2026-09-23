# Agent instructions: querycontract/answer

Contract types and pure builders. No reader, no I/O, no telemetry.

- Never import this package from `querycontract`; the parent is imported here
  for the truth envelope, so the reverse import is a cycle.
- `answer` is a common local-variable name in handler code. A local named
  `answer` in a file that imports this package shadows it; rename the local
  (see `buildPacketAnswer` in `query/investigation_packet_build.go`) rather
  than alias the import.
- `ClassifyAnswerTruth` must keep classifying a `no_backend_read` envelope as
  fallback at every level; `TestClassifyAnswerTruthPinsNoBackendReadToFallback`
  pins it.
- The answer_metadata JSON shape is versioned by
  `AnswerMetadataSchemaVersion`. Additive fields only; a breaking change needs
  a new version.
- The exported names keep their `Answer` prefix from the move out of
  `querycontract` (#6597). Renaming them is a separate change.

Verify with `cd go && go vet ./... && go test ./internal/query/... ./internal/queryplan/... -count=1`.
