# Infra read model rolling-upgrade fence (#6793)

Continues [6793-infra-read-model.md](6793-infra-read-model.md), which
covers the read model, its parity, route latency, retention lock, and drift
reconcile.

The reconcile alone left a stale window during a rolling upgrade: a new API
can record the backfill marker while an older ingester, projector, or
bootstrap-index binary still writes `content_entities` without deriving, and
readers would trust the table until the walk reached each repository. If the
reducer was also old, nothing repaired it. A reader-side "clean walk" gate
does not close this either, because it cannot see an old writer that is still
running.

Migration 109 therefore fences old writers in the database, the same approach
as migration 096's trigger fence against old reducer pods. The design and its
cost thresholds follow a separate arbiter ruling. Every connection opened by
`runtime.OpenPostgres` runs `SET eshu.infra_inventory_writer = 'derive'`
(`inventory.WriterSessionSQL`, through pgx's after-connect hook). Triggers on
`content_entities` skip sessions that carry that setting; any other session
that writes an infra-typed row (an older ingester, projector, bootstrap-index,
or reducer, or manual SQL) marks its repository in
`infra_resource_entity_dirty_repos` in the same transaction. INSERT and UPDATE
are statement-level triggers with transition tables, so a derive-aware session
evaluates one WHEN per statement and an unaware statement takes each mark
lock once. DELETE is row-level with the session test first, so a bulk
retention prune builds no transition tuplestore. An ungated TRUNCATE trigger
marks every mirrored repository. Readers (`inventory.ReadModelReady`) trust
the table only when the marker exists and no repository is marked. Each
reconcile cycle repairs marked repositories first, oldest first and counted
against the budget: lock, delete the mark, re-derive, commit (`fenced`). A
mark proves content changed without a derive, so it needs no second check.
A replica whose mark DELETE finds nothing (another replica or the backfill
discharged it) rolls back and reports nothing, so each mark is re-derived and
counted once (`TestWriterFenceLiveTwoReplicasRepairAMarkOnce`). The
backfill's `MirrorRepo` discharges a mark the same way; path derives never
do, because an unaware write can have changed any path.

Interleaving: the mark upsert is `ON CONFLICT (repo_id) DO UPDATE SET
marked_at = dirty.marked_at WHERE false`. It takes an existing mark's row
lock until the unaware write commits, but never rewrites the row, so a large
unaware write leaves no dead tuples (`TestWriterFenceLiveStatementShapes`
asserts the mark's `xmin` does not move). A repair's DELETE of the mark waits
for that write and, under Read Committed, re-checks the row and deletes it;
the re-derive statement that follows takes a new snapshot and reads the
write's rows. A write that starts after the repair's DELETE inserts a new mark
that survives the repair. With `DO NOTHING`, which takes no lock, a repair
could clear the mark and re-derive while the write's rows were still
invisible, leaving the table short with no mark.
`TestWriterFenceLiveRepairWaitsForAnOpenUnawareWrite` holds an unaware write
open, asserts the repair blocks on the mark (`pg_stat_activity`
`wait_event_type = 'Lock'`), commits, and asserts the table has the row and
the mark is gone. Each of these mutations fails a live test: `DO NOTHING`, a
rewriting upsert, a reader gate on the marker alone, a cycle that skips
marked repositories, and an insert trigger without the session predicate.

`TestWriterFenceLiveInvariantUnderConcurrentWritesAndRepairs` is the
concurrency proof: for 6 s an unfenced writer upserts and deletes infra rows of
one repository, a reconcile loop drains marks, and a REPEATABLE READ reader
checks, under one snapshot, that an unmarked repository's content digest equals
its table digest. Three runs under `-race`: about 2,900-3,500 unaware writes,
1,000-1,300 fenced repairs, and 1,270-1,450 unmarked snapshots each, with 0
violations, then no mark and equal digests after the writer stops. With the
upsert changed to `DO NOTHING`, three runs saw 690, 619, and 868 unmarked
snapshots over a stale table.

Lock hold: only unaware sessions take mark locks, and they hold one for the
rest of their own transaction. The older content writer autocommits each
300-row batch, about 6 ms each at production index cost in the shim below,
so another unaware writer of the same repository waits at most that long per
batch. An older reducer's retention prune holds the marks of every
repository it prunes for its whole transaction (2.3-4.9 s for a 50,000-fact
batch in the retention lock proof of the main note), once per prune batch, during the
rollout only. New sessions never take a mark lock.
Two older multi-repository transactions can deadlock on marks taken in
opposite orders; PostgreSQL aborts one and it retries on its next run.

A connection pooler that drops or does not forward session settings loses
the setting. That fails safe for this binary: its writes are marked, reads
stay on the graph, and the reconcile repairs them. A transaction-mode pooler
can also hand one binary's setting to another client's transaction, so an
older binary sharing such a pooler could write unmarked. Eshu's deployment
has no pooler; one placed in front of the DSN must forward
`eshu.infra_inventory_writer`, and a stripped setting shows as
`postgres.session_unfenced` at startup and `ok_unfenced_session` derives.

The ruling's statement-level bodies first used `SELECT DISTINCT repo_id,
clock_timestamp()`, which keeps one row per content row because the time
differs per row, so a multi-row unaware statement for one repository failed
with SQLSTATE 21000. The shipped bodies deduplicate repositories before
stamping the time, and the live shape test runs a 300-row unaware batch
upsert.

No-Regression Evidence: shim on a local PostgreSQL 18 with
`content_entities` and all of its production indexes (both trigram GINs),
the committed migration 109 fence DDL, and 50,000 `K8sResource` rows of one
repository (every row passes the label filter, the worst case) with
production-width `source_cache` and `metadata`. Passes: insert and re-upsert
as 300-row `INSERT ... ON CONFLICT (entity_id) DO UPDATE` statements (the
content writer's shape), deletes by `(repo_id, relative_path)` in 300-path
statements (5,000 rows), one bulk `DELETE ... WHERE repo_id` of the other
45,000 rows (a retention prune's shape), and one 50,000-row single-statement
insert (the review's worst case, previously 11.7 s unaware). Configurations
alternated in order each round, fresh table and a `CHECKPOINT` before each;
medians, microseconds per row against no triggers:

| Pass | no triggers | fenced (new binaries) | unfenced (older binaries) |
| --- | --- | --- | --- |
| insert, 5 rounds, production indexes | 1,057 ms | 1,062 ms, +0.09 us/row | 1,038 ms, -0.38 us/row |
| re-upsert | 1,148 ms | 1,146 ms, -0.05 us/row | 1,172 ms, +0.48 us/row |
| path deletes, 5,000 rows | 19.2 ms | 18.9 ms, -0.05 us/row | 27.3 ms, +1.62 us/row |
| bulk delete, 45,000 rows | 12.2 ms | 18.2 ms, +0.13 us/row | 88.2 ms, +1.69 us/row |
| one 50,000-row statement | 1,020 ms | 1,019 ms, -0.02 us/row | 1,045 ms, +0.51 us/row |
| insert, 7 rounds, PK only | 157 ms | 162 ms, +0.11 us/row | 166 ms, +0.19 us/row |
| re-upsert, PK only | 197 ms | 208 ms, +0.22 us/row | 212 ms, +0.30 us/row |
| path deletes, PK only | 14.6 ms | 15.1 ms, +0.11 us/row | 23.8 ms, +1.86 us/row |
| bulk delete, PK only | 10.5 ms | 16.2 ms, +0.13 us/row | 88.2 ms, +1.73 us/row |
| one 50,000-row statement, PK only | 120 ms | 138 ms, +0.36 us/row | 139 ms, +0.39 us/row |

With production indexes the fenced and unfenced insert and upsert deltas
are inside the round-to-round range (fenced insert 1,022-1,077 ms against
none 1,010-1,095 ms). The PK-only table resolves the absolute cost: at most
0.36 us per row for fenced sessions, where the only visible term is the
transition capture of a single 50,000-row statement (the writer batches 300
rows), and at most 1.86 us per row for unfenced ones, paid on deletes, where
the row-level trigger upserts once per deleted infra row. The ruling's
bounds were 0.5 and 3.0 us per row. The unaware single-statement insert that
took 11.7 s with the row-level rewriting upsert now costs 1,045 ms against
1,020 ms. Every fenced run left 0 marks; every unfenced run left exactly 1.

`TestContentWriterLiveInfraFenceCost` runs the real `ContentWriter.Write`
over the full bootstrap schema: 50,000 `K8sResource` entities of one
repository in 5,000 files, on a derive-aware connection with the triggers
installed against the same schema with them dropped, five interleaved rounds,
a first Write (insert path) and a second Write of the same entities (update
path). Medians against triggers dropped, with the fenced and dropped ranges:

| Entity batch | Path | triggers dropped | fenced | Delta |
| --- | --- | --- | --- | --- |
| 300 (default) | insert | 3.23 s (2.69-4.06) | 2.81 s (2.67-3.10) | -12.9% |
| 300 (default) | update | 2.26 s (2.10-4.70) | 2.49 s (2.14-3.97) | +10.1% |
| 4,000 (near the parameter limit) | insert | 2.10 s (2.09-2.35) | 2.14 s (2.08-2.40) | +2.0% |
| 4,000 (near the parameter limit) | update | 2.37 s (2.29-2.74) | 2.48 s (2.37-2.87) | +4.8% |

The whole Write costs about 45-65 us per row, and its round-to-round range
is 12-100%, so the Write-level deltas, of both signs, sit inside the noise;
the PK-only shim above resolves the fence's own cost at 0.1-0.4 us per row.
No fenced Write left a mark.

After a rollback to a release before migration 109 the triggers stay,
because migrations are recorded and not reverted. Every older binary is then
an unaware writer: it pays the unfenced column above (at most about 2 us per
deleted infra row, about 0.5 us per upserted one) and accumulates marks that
nothing repairs until a release with the reconcile runs again. That older
release does not read the table, so the marks are harmless there. The upgrade
guide (`docs/public/deploy/kubernetes/upgrades-rollbacks.md`) says to keep the
triggers, and how to recover if they were dropped: a recorded migration is not
re-applied, so the operator removes its record and the backfill marker before
upgrading again.

Observability Evidence: `eshu_dp_infra_inventory_reconcile_total{outcome="fenced"}`
counts repositories repaired after an unaware write; span attribute
`eshu.infra_inventory.repos_fenced` and log event
`infra_inventory.reconcile.fenced` (warn, with `repo_id`) name them. Gauges
`eshu_dp_infra_inventory_dirty_repos` and
`eshu_dp_infra_inventory_dirty_oldest_age_seconds`, recorded every reconcile
cycle whether or not the marker exists, and the `/admin/status` field
`infra_inventory` (`state` of `not_installed`, `backfilling`, `fenced`, or
`ready`, with the count, oldest age, and the reason reads are on the graph)
say why unscoped reads are on the graph. A derive whose own connection lacks
the setting counts `eshu_dp_infra_inventory_derives_total{outcome="ok_unfenced_session"}`
and logs `infra_inventory.derive.unfenced_session` at ERROR, and each writer
binary logs `postgres.session_unfenced` at startup when its session lacks it.
