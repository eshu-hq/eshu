# internal/content/shape

## Read First

1. `go/internal/content/shape/README.md`
2. `go/internal/content/shape/doc.go`
3. `go/internal/content/shape/materialize.go`
4. `go/internal/content/shape/materialize_labels.go`
5. `go/internal/content/shape/source_cache.go`
6. `go/internal/content/README.md`

## Package Rules

- `contentEntityBuckets` order is persisted output behavior. New buckets MUST
  be appended unless a migration and storage-diff plan exists.
- Materialization output MUST stay deterministic: line number, then label, then
  name. Do not remove or weaken the sort.
- `Materialize` MUST reject blank repository IDs and blank file paths.
- `entityLabelForBucket` MUST preserve the `module_kind =
  protocol_implementation` rewrite for Elixir protocol implementations.
- `source_cache` truncation MUST stay UTF-8 safe and MUST write
  `source_cache_truncated`, `source_cache_original_bytes`, and
  `source_cache_limit_bytes` metadata when it cuts a value.
- This package produces `content.Materialization` only. Do not import storage,
  SQL, graph, queue, or telemetry packages here.
- Parser evidence shape changes MUST be carried through tests for file
  metadata, entity records, and source-cache fallback behavior.

## Proof

- Run `cd go && go test ./internal/content/shape -count=1` for package changes.
- Run `cd go && go vet ./internal/content/shape` when exported surface or docs
  move.
- Run `go run ./cmd/eshu docs verify ../go/internal/content --limit 1400 --fail-on contradicted,missing_evidence`
  for docs changes under `internal/content`.
