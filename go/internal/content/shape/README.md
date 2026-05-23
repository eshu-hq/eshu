# Content Shape

## Purpose

`shape` turns parser-emitted file and entity payloads into
`content.Materialization` rows for the content store. It centralizes
entity-bucket label mapping, canonical entity IDs, `source_cache` derivation,
and byte limits for low-signal snippets.

## Ownership Boundary

This package owns content shaping only. It does not write Postgres rows, enqueue
work, emit facts, or write graph state. Callers in ingester/projector paths own
runtime orchestration and persistence.

## Exported Surface

See `doc.go` and `go doc ./internal/content/shape` for the contract. Callers use
`Materialize`; it rejects blank repository IDs and blank file paths.

## Telemetry

None. Callers wrap `Materialize` with their own runtime metrics, spans, and
status reporting.

## Gotchas / Invariants

- `contentEntityBuckets` order is persisted output order. Add new buckets at
  the end to avoid row churn.
- Terraform bucket labels must stay aligned with parser and projector mapping.
- Elixir `defimpl` rows become `ProtocolImplementation` when parser metadata
  carries `module_kind == "protocol_implementation"`.
- `source_cache` prefers parser-provided source for code labels, then bounded
  file-body ranges, then non-code source fields.
- Variable snippets are truncated UTF-8 safely at 4096 bytes and record
  truncation metadata.
- Entity ordering is deterministic: line number, label, then name.

## Focused Tests

```bash
cd go
go test ./internal/content/shape -count=1
go vet ./internal/content/shape
go doc ./internal/content/shape
```

## Related Docs

- `go/internal/content/README.md`
- `docs/public/architecture.md`
- `docs/public/reference/local-testing.md`
