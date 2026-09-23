# Agent instructions: querycontract/evidence

Contract types and pure helpers. No reader, no telemetry.

- This is a base leaf: the parent `querycontract` imports it. Never import
  `querycontract` from here.
- `EvidenceCitationHandleKey` is both a type and a method name on
  `EvidenceCitationHandle`; a textual rename that ignores the leading `.` will
  break the method calls.
- Hot call sites that name these types are pinned by source hash in
  `go/internal/queryplan/testdata/query-source-coverage.yaml`. Run
  `go test ./internal/queryplan/... -count=1` after any change.

Verify with `cd go && go vet ./internal/query/... && go test ./internal/query/... ./internal/queryplan/... -count=1`.
