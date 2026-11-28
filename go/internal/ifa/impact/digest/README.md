# ifa/impact/digest

## Purpose

`digest` is the Postgres truth read of the supply-chain impact family of the Ifá fault and
determinism gates (#10880 PR-C2). The impact domain writes no graph, so the graph-dump digest cannot see
it; its output is the supports table and the canonical winners table. `DigestSQL` hashes both without the
run-naming columns, and `Read` returns the digest with the row counts it covers and refuses an empty family.

Two readers share the one statement:

- the in-process family, `go/internal/storage/postgres/supply_chain_impact_family_live_test.go` and its
  faults file, which drive the real queue and handler at 1, 2 and 4 workers;
- the Compose cell `cell_impact_family`, through `eshu-ifa impact-digest` (`go/cmd/ifa/impact_digest.go`).

`Cassettes` lists the three committed cassettes the family drives, in commit order. The Compose cell's
shell list is pinned to it by `go/cmd/ifa/impact_digest_test.go`.

## Ownership Boundary

The package holds a statement, a list and a read; it opens no connection and imports no driver. Callers pass
a `Queryer`.

## Performance And Observability

No runtime path: the read runs once per gate run against a disposable database.
