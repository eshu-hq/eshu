# Content

## Purpose

`content` defines the source-local content write contract used by the projector
and any future write path that persists file bodies and entity rows to Postgres.
It owns the `Writer` port, the value types that flow through that port, the
canonical content-entity identifier, and the runtime tunable for batch width.
The Postgres storage adapter lives in `internal/storage/postgres` and implements
`Writer`; this package has no Postgres dependency.

## Ownership boundary

This package owns the write port, content value types, stable entity ID helper,
batch-width config, and in-memory test writer. It does not own parser shaping,
the Postgres schema, SQL, connection pools, graph writes, or queue behavior.
`internal/content/shape` builds `Materialization` values; `storage/postgres`
persists them.

## Exported surface

See `doc.go` and `go doc ./internal/content` for the godoc contract. Callers
depend on `Writer`, `Materialization`, `Record`, `EntityRecord`, `Result`,
`CanonicalEntityID`, `WriterConfig`, `LoadWriterConfig`, and `MemoryWriter`.

## Dependencies

- `golang.org/x/crypto/blake2s` for `CanonicalEntityID`. No internal-package
  imports; this package is a leaf in the dependency graph.

## Telemetry

None directly. Postgres writer adapters in `internal/storage/postgres` add the
duration histograms and batch-size counters required by the observability
contract.

## Gotchas / invariants

- `CanonicalEntityID` lower-cases `entityType` and trims whitespace from every
  input before hashing. Callers that pre-trim or pre-lower differently will
  produce divergent IDs.
- `Clone` methods (`Record.Clone`, `EntityRecord.Clone`, `Materialization.Clone`)
  must be called before retaining inputs across async boundaries. `MemoryWriter`
  always stores a clone, not the raw input.
- `LoadWriterConfig` returns an error when `ContentEntityBatchSizeEnv` is set to
  a non-positive integer or a value above `MaxContentEntityBatchSize` (4000).
  A zero value from the env means "use the adapter default."
- Test files (`postgres_writer_test.go`, `writer_test.go`,
  `writer_config_test.go`) are in `package content_test` — external test
  package. Do not move them to `package content` without re-checking export
  visibility.

## Focused tests

```bash
cd go
go test ./internal/content -count=1
go vet ./internal/content
go doc ./internal/content
```

## Related docs

- `go/internal/content/shape/README.md` — shaping layer that builds `Materialization`
- `docs/public/architecture.md` — pipeline and Postgres content store role
- `docs/public/reference/local-testing.md`
