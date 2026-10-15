# infra/search

## Purpose

`search` maintains the derived `infra_search_key` node property for the infra
resource search (#7245). The search reads twelve free-text properties on 27
infra labels. Without an index, every request label-scans all of them. The key
joins those twelve properties into one string that one Neo4j keyword FULLTEXT
index (`infra_search_key_ft`, owned by `internal/graph`) serves through a
wildcard query.

This package is the write side only. The query handler does not read the key
in this release (PR-1 is dark). The read cutover is #7245 PR-2.

## Contract

- `Clause(alias)` is the one `SET` clause that writes the key. It reads only
  the node's own properties, never a parameter.
- Each part is the property when it is a non-null string, and the empty string
  otherwise. The resource type is `coalesce(resource_type, data_type)`, as in
  the predicate.
- Parts are joined with U+001F. The key is a superset filter: every field
  value is in it verbatim. The reader must re-apply the unchanged predicate.
- `infra_search_ovf` is always written: `'T'` when the joined key is longer
  than `graph.InfraSearchKeyMaxChars` (8,000 code points), and the key is then
  null; `'F'` otherwise. It is a string because a FULLTEXT index indexes only
  strings. A node with no `infra_search_ovf` was never keyed.
- The cap is load-bearing. The keyword analyzer makes the whole key one Lucene
  term. 8,000 code points at 4 bytes is 32,000 bytes, below the 32,766-byte
  term limit. A longer term does not reject the row: the commit fails after
  the node is written, and the index goes FAILED until it is rebuilt.
  `TestInfraSearchKeyIsWrittenOnlyByTheClause` keeps every other writer away
  from both properties.

## Dialect

`Statement(dialect, label, alias, cypher)` appends the clause when `dialect` is
`On` and `label` is an infra search label. `Off` returns `cypher` byte for
byte. `DialectForBackend` maps `graph.SchemaBackendNeo4j` to `On` and
`graph.SchemaBackendNornicDB` to `Off`. The zero `Dialect` is invalid, and
`MustDialect` panics on it in the writer `WithDialect` constructors.

NornicDB is `Off` because the pinned image does not evaluate `IS :: STRING`.
It stores the expression text as the property value. The schema gives
NornicDB no key index either.

## Backfill

`Backfill` runs one statement per label. It selects nodes whose stored key or
flag disagrees with their fields and writes them with `Clause` in
`CALL (n) { ... } IN TRANSACTIONS OF $batch_size ROWS`. It writes no other
property, and a second run writes nothing. `eshu-bootstrap-data-plane` runs it
on Neo4j after graph DDL and before the schema marker.

`CountUnkeyed` returns the per-label count of nodes without the flag. PR-2
gates its readiness marker on that count and a fresh backfill.

`OversizedKeyCount` (statement: `OversizedKeyCountCypher`) counts keys longer
than the cap. Bootstrap runs it before the index DDL and stops when it is not
zero, because such a key would fail the index.

All three need an auto-commit runner (`Runner`).

## Telemetry

`Backfill` logs `bootstrap.graph.infra_search_key.label_backfilled` per label
(`label`, `nodes_keyed`, `overflow_count`, `batch_size`, `duration_seconds`)
and `bootstrap.graph.infra_search_key.backfilled` once (`label_count`,
`nodes_keyed`, `overflow_count`, `duration_seconds`). The bootstrap binary has
no meter provider, so there is no metric.

## Verification

- `go test ./internal/storage/cypher/infra/search -count=1`
- The census guard and writer tests in `internal/storage/cypher`.
- The live Neo4j proof in
  `internal/storage/cypher/search_key_live_test.go` (Neo4j only; the scale
  measurement in `search_key_scale_live_test.go` runs with
  `ESHU_INFRA_SEARCH_KEY_SCALE=1`).

Evidence: `docs/internal/evidence/7245-infra-search-key-dark.md`.
