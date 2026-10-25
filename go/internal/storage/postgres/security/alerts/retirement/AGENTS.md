# AGENTS.md — security-alert scope retirement reads

## Read first

1. `README.md` and `doc.go` in this directory.
2. `go/internal/coordinator/security/alert/scopes.go` for the planner that
   consumes `RetirementCandidates`.
3. `go/internal/storage/postgres/migrations/001_ingestion_scopes.sql` and
   `230_security_alert_target_read_states.sql` for the tables it reads.

## Invariants

- Keep the read indexed: parent equality on `ingestion_scopes_parent_idx`,
  base by primary key, read state by primary key. A predicate change needs the
  live EXPLAIN proof rerun (`TestShardRetirementPlannerReadPlanLive`).
- The read skips a scope whose active generation's read state has
  `repositories_total = 0`. Every retirement commits 0; a collection with 0
  (no allowlisted repository of the organization) holds no alert fact, because
  the collector filters org alerts through `RepositoryAllowed`, so skipping it
  leaves no readable row. The zero count is a skip condition, not the
  retirement marker; the marker is `shard.RetirementFreshnessHint`.
- Return query and row errors; an empty list must mean nothing to retire.
- Never import the parent `postgres` package.
