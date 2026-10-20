# Retirement plan tables

This package holds the Deliverable 2 retirement plan tables (#8659): the
per-domain graph-retraction classification (`plan.go`), the per-table
Postgres disposition census (`census.go`), and the partition-lease domain
classification for the 2b' barrier wait (`plan.go`).

All three tables are closed. The tests fail when a reducer domain, a
schema table, or a lease writer is added without a classification, so new
writers cannot slip past the retirement path silently. When a test here
fails on a new row, classify the row with a reason that cites its source
check — a row without one is a guess.

## For agents

- Read `doc.go` for the package contract, then the table you are
  changing.
- The domain table's union is `KnownDomains` plus `ProjectionDomains`
  plus `DomainSearchVectorBuild` (see the test comment for why the last
  one is explicit).
- The Postgres dispositions mirror
  `docs/internal/design/7766-repository-retirement-table-census.md`; the
  live census test in `go/internal/storage/postgres` proves coverage
  against the migrated schema.
- Keep files under 500 lines; split the table before it approaches the
  limit.
