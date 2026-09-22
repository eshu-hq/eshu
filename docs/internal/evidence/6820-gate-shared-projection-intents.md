# Read-API latency gate: seed shared_projection_intents (#6820)

Change: `go/cmd/read-api-latency-gate` seeds `shared_projection_intents`
(`BuildSharedIntentPlan`/`SeedSharedIntents`, `-shared-intent-count`,
`GATE_SHARED_INTENT_COUNT`, default 2,500,000 rows) after the scope,
generation and work-item seeds: completed intents across every reducer
projection domain (`contract.ProjectionDomains()`, 14 domains) with the
newest 1% left pending, one row per second of created_at, streamed to
`pgx.CopyFrom` from a pure row-from-index plan so the corpus is never held in
memory. The exact-count read-back (`VerifyRelationalCounts`) covers the table
(the first live run failed there because the counter's table allowlist did
not, now guarded by `TestSeededRelationalTablesAllowSharedIntents`). The
per-route work budgets in `testdata/benchmarks/read-api-route-work-budgets.txt`
were regenerated from the GREEN work report by
`scripts/refresh-read-api-work-budgets.sh`; every changed row stayed inside
the 1.5x growth ratchet (status routes 58,512 to 59,619 blocks, +1.9%).

Theory shim (postgres:18-alpine, production DDL from
`go/internal/storage/postgres/shared_intents.go`, seven indexes): at 300k rows
with pending rows spread uniformly the shipped pending CTE cost 3,045 buffers
against 6,170 for a non-sargable variant (2x, under the gate's 3x margin);
with pending rows clustered at the tail as on a live instance, 1M rows read
358 buffers by bitmap index scan, and at 2.5M rows (51,043 heap pages,
1,127 MB with indexes) the shipped CTE was an index-only scan while the
variant read 51,119 buffers. Seeding 2.5M rows took about 54 s. The corpus is
sized so that one full scan of the heap exceeds the 3x blocks budget of the
status routes that read it; 1M rows (about 20k pages) would not.

Live gate, both directions on one seeded stack (`scripts/verify-read-api-latency-gate.sh --keep`, 800 scopes, 150k nodes per infra label, 150k IaC facts, 2.5M intents, 20 requests per route, host Docker Compose postgres:18-alpine plus the pinned NornicDB image):

Performance Evidence: GREEN (shipped `domainBacklogQuery`, head 1587100b93) — all 65 exercised routes within budget, gate exit 0; the status routes read 19,873 blocks per request (`/status/operations` 24,689; `/index-status` 18,561) at p95 72–80 ms, up from about 19,500 before the seed, i.e. the pending-only aggregate costs a few hundred blocks over 2.5M rows. RED (`eshu-api` built from the same head with the pending predicate rewritten to `COALESCE(completed_at, 'infinity') = 'infinity'`, same rows, non-sargable, started against the kept stack and swept with `-skip-seed`) — 14 routes exceeded their Postgres work budget, gate exit 1: `/status/collectors` 76,897 blocks against 58,512, `/status/operations` 81,715 against 72,960, `/index-status` 75,591 against 54,578, p95 238–250 ms; calls and rows unchanged. The RED variant would also breach the regenerated budgets (59,619 / 74,067 / 55,683).

No-Observability-Change: the gate adds no metric, span, log key or status field to any service; its own stderr gains one `seeding N shared projection intents across D domains (newest P pending; ...)` progress line, and the status routes keep every signal they had.

Root-Cause Evidence: the table was unguarded because the seed never wrote it, so `shared_projection_pending` read zero blocks on every gate run; the RED run above is the observation — the same unindexed variant that costs 51k extra blocks on the seeded corpus costs nothing on an empty table, so no work budget could have moved before this change.
