# Agent instructions: querycontract/visualization

Contract types and pure helpers. No reader, no telemetry.

- Never import this package from `querycontract`; that would be a cycle.
- `query/visualization` is a different package with the same name; alias the
  import only where both meet in one file.

Verify with `cd go && go vet ./internal/query/... && go test ./internal/query/... ./internal/queryplan/... -count=1`.
