# AGENTS.md — `go/internal/reducer/retirement`

Closed plan tables for operator-driven repository retirement (#8659).
They classify; they never execute. The staged runner (a later lane) reads
them; the tests fail a new domain, table, or lease writer until someone
classifies it here.

## Rules

- Every row needs a reason that cites its source check (retract path,
  writer contract, or schema DDL). No reason, no row.
- `retract_by_repo` requires an existing repo- or tombstone-scope-keyed
  retract entry point. Do not assert one that does not exist.
- `removed_by_node_retract` requires the edges to be incident only to
  nodes the G5/G5w DETACH DELETEs remove.
- `not_repository_scoped` requires the non-repo identity (cloud uid,
  cluster key, CI scope) or the Postgres-only writes, verified in the
  handler — never assumed from the domain name.
- Lease table: `cfg.Domain` (shared worker) rides the derived range;
  only literals go in the map. A new `ClaimPartitionLease` caller fails
  `TestRetirementLeaseDomainsMatchLeaseWriters` until classified.
- Keep every file under 500 lines. Go doc comments on all exported
  identifiers. MIT SPDX header on every file.
