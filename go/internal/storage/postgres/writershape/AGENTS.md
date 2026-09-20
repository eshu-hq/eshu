# Agent instructions — writershape

Read `doc.go` for the contract before changing the marker SQL.

- DDL stays `CREATE TABLE IF NOT EXISTS`; every mutation stays an `UPDATE`
  or an `INSERT ... ON CONFLICT DO NOTHING`. Never a bare `INSERT`.
- `applied_version` moves forward only, and only after the upgrade
  refinalize succeeds. `claimed_at`/`claimed_version` converge concurrent
  starters; never advance the applied marker on a failed refinalize.
- No new tables or indexes without an `EXPLAIN ANALYZE`-backed reason.
- Claim-predicate changes need the live concurrency proof
  (`TestGraphWriterShapeClaimConcurrencyLive`) green against real Postgres,
  not just the scripted unit tests: the second claimant must block on the
  winner's row and re-evaluate against it.
