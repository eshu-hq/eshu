# AWS Reducer Contract

## Purpose

`internal/reducer/aws` records the reducer-facing contract for AWS cloud
materialization. It names the AWS runtime-drift reducer components and
readiness checkpoints used by fixtures, docs, and downstream planning.

## Ownership Boundary

This package owns contract values only. It does not collect AWS data, emit
facts, project source-local graph rows, run reducer workers, or answer queries.
AWS observation lives in collector packages; graph writes and intent handling
live in projector/reducer runtime code.

## Exported Surface

See `doc.go` and `go doc ./internal/reducer/aws`. The package exposes runtime
contract structs, default contract helpers, validation, and defensive-copy
templates.

## Telemetry

None. Runtime AWS materialization telemetry belongs to projector, reducer,
queue, and graph-write code.

## Gotchas / Invariants

- Contract helpers return copies; callers must not mutate shared defaults.
- Validation rejects blank component names or checkpoint fields.
- This package does not prove readiness at runtime. It records the contract the
  runtime must publish.
- Cross-source cloud runtime truth remains reducer-owned and must stay
  provenance-backed.

## Focused Tests

```bash
cd go
go test ./internal/reducer/aws -count=1
go doc ./internal/reducer/aws
go run ./cmd/eshu docs verify ../go/internal/reducer/aws --limit 1000 \
  --fail-on contradicted,missing_evidence
```

## Related Docs

- `go/internal/reducer/README.md`
- `go/internal/collector/awscloud/README.md`
- `docs/public/architecture.md`
