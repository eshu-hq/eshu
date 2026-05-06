# AGENTS.md — internal/content/shape guidance for LLM assistants

## Read first

1. `go/internal/content/shape/README.md` — ownership boundary, pipeline
   position, and invariants
2. `go/internal/content/shape/materialize.go` — `Input`, `File`, `Entity`,
   `Materialize`, bucket table, entity ordering
3. `go/internal/content/shape/source_cache.go` — `entitySourceCache`,
   `limitEntitySourceCache`, `truncateUTF8ByBytes`
4. `go/internal/content/shape/materialize_labels.go` — `entityLabelForBucket`,
   `indexedEntity`
5. `go/internal/content/README.md` — the `Materialization` and `EntityRecord`
   shapes this package produces

## Invariants this package enforces

- **Fixed bucket order** — `contentEntityBuckets` in `materialize.go` is a
  stable ordered list. Inserting a new bucket anywhere except the end changes
  the persisted row order for existing entities and produces downstream churn.
  Always append.
- **Deterministic output** — entities are sorted by `lineNumber()`, then label,
  then `Name` before building `content.EntityRecord` values. Tests assert this
  order; do not remove the sort.
- **Protocol-implementation rewrite** — `entityLabelForBucket` promotes `Module`
  rows to `ProtocolImplementation` when `module_kind == "protocol_implementation"`
  is present in `Entity.Metadata`. This handles Elixir `defimpl` at
  `materialize_labels.go:3`.
- **Variable byte cap** — `entitySourceCacheByteLimits` in `source_cache.go`
  caps `Variable` snippets at 4096 bytes. `limitEntitySourceCache` truncates
  and writes metadata keys `source_cache_truncated`, `source_cache_original_bytes`,
  `source_cache_limit_bytes`. Do not raise the cap without confirming the
  Postgres `source_cache` column can hold the new maximum.
- **UTF-8 safety** — `truncateUTF8ByBytes` must not split a multi-byte rune.
  Any change that truncates `source_cache` must go through this helper.
- **No storage dependency** — this package must not import `database/sql`,
  `pgx`, or any `internal/storage` sub-package. It produces a
  `content.Materialization`; storage is the caller's concern.

## Common changes and how to scope them

- **Add a new entity bucket** → add an `entityBucketMapping` entry at the end
  of `contentEntityBuckets` in `materialize.go`. Add the label to
  `trailingNewlineLabels` and `sourceFieldContainsCode` if appropriate. Add a
  test case in `materialize_test.go`. Run
  `go test ./internal/content/shape -count=1`.

- **Add a byte cap for a new label** → add an entry to
  `entitySourceCacheByteLimits` in `source_cache.go`. The cap applies to the
  final `source_cache` string after snippet extraction. Add a test in
  `source_cache_test.go` that verifies the metadata keys are written on
  truncation.

- **Change the `File` or `Entity` struct** → update the struct in
  `materialize.go`, propagate the field through `materializeFile` and
  `materializeEntities`, update `content.EntityRecord` or `content.Record` if
  storage needs the value, add a test. Check `normalizeFileMetadata` — it maps
  specific `File` fields into the `Metadata` string map.

- **Add a label rewrite rule** → add a branch in `entityLabelForBucket` in
  `materialize_labels.go`. Include a `materialize_test.go` case that confirms
  the rewrite fires and that non-matching entities keep their original label.

## Failure modes and how to debug

- Symptom: entities appear in wrong order in content-store rows → likely cause:
  sort was removed or `lineNumber()` returns 0 for items with `LineNumber == 0`.
  `indexedEntity.lineNumber()` clamps to 1 as the floor — verify this is still
  the case.

- Symptom: `Variable` source_cache rows unexpectedly large → likely cause: the
  4096-byte cap in `entitySourceCacheByteLimits` was removed or the cap was
  raised. Check `source_cache_truncated` metadata key is present on large rows.

- Symptom: Elixir protocol implementations stored as `Module` → likely cause:
  parser is not setting `module_kind = "protocol_implementation"` in
  `Entity.Metadata`. Confirm the parser output first before changing
  `entityLabelForBucket`.

- Symptom: `Materialize` returns error on empty `RepoID` → this is expected
  behavior. All callers must supply a non-empty `RepoID`.

## Testing

Gate: `cd go && go test ./internal/content/shape -count=1`

Key test files:

- `materialize_test.go` — bucket coverage, entity ordering, label rewrites
- `materialize_analytics_test.go` — analytics model bucket entities
- `materialize_sql_test.go` — SQL entity buckets
- `source_cache_test.go` — `Variable` byte cap, UTF-8 truncation safety,
  function bodies left unchanged
