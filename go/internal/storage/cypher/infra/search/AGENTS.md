# AGENTS.md - infra/search

## Read first

1. `doc.go` for the contract.
2. `README.md` for the dialect, overflow, and backfill rules.
3. `../../AGENTS.md` for the writer package that uses this one.

## Invariants

- One clause. Every writer of the twelve searched properties on an infra
  search label appends `Clause` through `Statement`. Never compute the key in
  Go or pass it as a parameter. `TestInfraSearchKeyCensus` in
  `internal/storage/cypher` fails when a writer skips it.
- The field list must equal the properties
  `deployment.InfraResourceFreeTextPredicate` reads, with the same
  `coalesce(resource_type, data_type)` order.
  `TestInfraSearchKeyFieldsMirrorFreeTextPredicate` enforces it.
- Guard each part with `IS :: STRING NOT NULL`. On the pinned Neo4j,
  `null IS :: STRING` is true.
- The clause computes the joined key once: `WITH alias, <joined> AS
  infra_search_joined_key`, then one `SET` of both properties from that row
  variable. The over-cap value never reaches the node. The `WITH` keeps only the
  node variable, so `Statement` appends the clause last. The index-key guard
  reads the result (`graph.UnanalyzedIndexWrites` reports nothing; the writer
  tests assert it).
- `Off` must return the statement byte for byte. NornicDB cannot evaluate the
  clause.
- Never write `infra_search_key` or `infra_search_ovf` outside `Clause` and
  `Backfill`. A key past the cap fails the FULLTEXT index, not the row.
  `TestInfraSearchKeyIsWrittenOnlyByTheClause` enforces it.
- The flag is the string `'T'` or `'F'` (`graph.InfraSearchOverflowYes`,
  `graph.InfraSearchOverflowNo`), always written by the clause.
- Do not import `internal/storage/cypher` from here; it imports this package.

## Common changes

- Changing the predicate's fields: change `fields` and `joinedExpr` in the
  same PR, then re-run the live proof and the backfill on every Neo4j store.
- Raising `graph.InfraSearchKeyMaxChars`: do not, unless the Lucene term limit
  on the pinned Neo4j is re-measured. 8,000 code points is the largest value
  whose worst-case UTF-8 size fits 32,766 bytes.

## Verification

`go test ./internal/storage/cypher/infra/search ./internal/storage/cypher
-count=1`, then the live test named in `README.md`.
