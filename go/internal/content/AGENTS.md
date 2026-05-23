# internal/content

## Read First

1. `go/internal/content/README.md`
2. `go/internal/content/doc.go`
3. `go/internal/content/writer.go`
4. `go/internal/content/writer_config.go`

## Package Rules

- `CanonicalEntityID` is a persisted identity contract. It MUST keep the
  `content-entity:e_<12-hex>` format, BLAKE2s digest, input order, whitespace
  trimming, and lower-casing of `entityType` unless a storage migration is
  designed and reviewed first.
- `Record`, `EntityRecord`, and `Materialization` MUST remain clone-safe across
  async boundaries. Add reference fields only with matching `Clone` coverage and
  tests.
- `Writer` is the only storage boundary in this package. Do not import
  `database/sql`, `pgx`, or `internal/storage/*` here.
- `LoadWriterConfig` MUST reject invalid batch sizes. Raising
  `MaxContentEntityBatchSize` requires proof that the widest Postgres upsert
  stays under the driver bind-parameter limit.
- Adding fields to content records is a cross-boundary change. Update the
  Postgres writer, tests, and any docs for user-visible stored shape changes in
  the same PR.

## Proof

- Run `cd go && go test ./internal/content -count=1` for package changes.
- Run `cd go && go vet ./internal/content` when exported surface or docs move.
- Run `go run ./cmd/eshu docs verify ../go/internal/content --limit 1400 --fail-on contradicted,missing_evidence`
  for docs changes in this package.
