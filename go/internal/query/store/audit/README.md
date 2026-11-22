# Query store audit (package audit)

## Purpose

Every capability in `specs/capability-matrix.v1.yaml` declares the stores that
answer its reads (`answering_stores`). Declaration completeness is checked by
`cmd/capability-inventory -mode verify`. This package checks that the
declarations are honest: it fails when a read uses a store the capability did
not declare. It is the layer-2 audit of the #10880 design record.

## How it works

- `Recorder` implements `db.ReadStore` and returns empty results. A probe in
  `probes_test.go` builds a production store on it and drives one branch, then
  reads back the SQL and arguments the store actually issued.
- `Classify` turns statements into store tokens (`fact_records_reducer`,
  `fact_records_collector`, `read_table:<table>`, `content_search`). It resolves
  fact kinds from literals, `IN` lists, and bind arguments, ignores CTE names,
  comments, string literals, lifecycle joins, and branches a literal `AND FALSE`
  closes, and reports an unresolvable `fact_records` read as unresolved.
- `Undeclared` returns the tokens a declaration does not name.
- `Census` classifies the SQL string literals of every non-test Go file per
  package. It needs the set of tables the schema creates (`KnownRelations`) so a
  CTE alias or a word in prose never reads as a table.

## Waivers

`testdata/probe-waivers.txt` lists capabilities with no probe yet, and
`testdata/census-waivers.txt` lists query packages with no census owner yet.
Both may only shrink: `probeWaiverCeiling` and `censusWaiverCeiling` in
`audit_test.go` pin their length, and a stale entry fails. Regenerate the lists
with `ESHU_UPDATE_STOREAUDIT_WAIVERS=1 go test ./internal/query/store/audit`,
which records what is unprobed or unowned today.

## Not covered

A probe sees only the branches it drives, and the census cannot tell which
capability a literal serves. Graph (`MATCH`) and content-store reads are not
recorded yet; those capabilities are waived until a graph recorder lands.
