# Generation Tag on content_files (#7760)

The permanent fix for the #7609 ahead-manifest corner. `content_files` gains
`generation_id` (migration 169): the content writer stamps every stored file
row with the materialization's generation id, and the two producer manifest
reads (`PackageManifestsQuery`, `GoModuleManifestsQuery` in
`go/internal/storage/postgres/code/producers/store.go`) bind content to the
generation that wrote it instead of inferring dirt from proxy signals.

The tag rule is fail-safe and status-agnostic: a row reads clean only when
its tag names a generation row with `activated_at IS NOT NULL`. A NULL tag
(legacy or mixed-deploy write), a dangling tag (generation retention-pruned;
there is deliberately no foreign key), an empty tag, or a tag naming a
never-activated generation all read as a dirty NULL row the anchored scan
must visit, never dropped. A tag from an older activated generation is
clean by carry-forward: the active generation never rewrote the path, so the
stored bytes are still the active truth. Do not compare `superseded_at` to
`activated_at`: Ack stamps the prior active with the same clock, which would
dirty every scope with two activations.

The #7776 dirty predicate is partially replaced, not removed: per-row tags
cannot see row-less scopes, so the manifest-less generation leg stays. A
scope with no stored manifest but a never-activated generation still
resolves dirty through that leg (arbiter condition C1). The timestamp and
generation-exists legs over stored manifests are gone; the backfill test
pins the precision gain (an unrelated unactivated generation no longer
dirties a clean scope).

The backfill attributes a legacy row to the scope's active generation only
where all hold: (i) the scope has zero generation rows with `activated_at IS
NULL`, (ii) the active row exists with status `active` and activated, (iii)
the row's `indexed_at` is at or before the activation (per-row guard), (iv)
exactly one repository scope maps the repo key. In the ahead corner the
writing generation has NULL `activated_at`, so (i) excludes exactly the
dangerous scopes; backfilling them to active would bless ahead content as
active truth (the #7609 bug). Post-hole scopes whose signal row was
retention-pruned pass (i) but fail (iii): they stay NULL (dirty), no worse
than the pre-tag read. Scopes failing any clause converge as generations
rewrite their manifests.

Residuals: a manifest deleted by a write with no generation row leaves no
row and no tag, and is uncatchable (second-order: crash-window write plus
ambiguity-relevant load). A behind-skewed writer clock on a
post-retention-hole scope can wrong-clean through clause (iii) exactly as
the timestamp leg this tag replaces could; live unactivated generations are
still caught by the tag regardless of skew.

Migration proof (local PostgreSQL 18, 2026-10-09). It timed each statement
alone, on a fixture without the production `content_files` index set; see
"Production Index Set Measurement (#7961)" below. 1,000,000-row
`content_files` fixture, 3,000 repository scopes, 1% planted never-activated
generations, 10% of filler rows indexed after the activation. ADD COLUMN:
4.0 ms (catalog-only, no rewrite). Backfill UPDATE: 30.9 s, tagging 890,300
rows and skipping 109,700 (30 pending-scope manifests plus 9,970
pending-scope filler rows by clause i, 99,700 late filler rows by clause
iii; the skip counts cross-check the seed arithmetic exactly). ANALYZE:
1.0 s. These are individual statement timings, not proof that readers could
run during the original bootstrap call. Its single `ExecContext` sent ALTER,
UPDATE, and ANALYZE in one multi-statement message. PostgreSQL kept the ALTER's
ACCESS EXCLUSIVE lock until the message's implicit transaction ended, across
the long UPDATE, so concurrent reads could be blocked. Re-runs are idempotent via the `WHERE
generation_id IS NULL` guard, which also never clobbers tags the new writer
stamps mid-migration.

The migration executor now recognizes only the shipped migration 169 full
variant at its frozen path and SHA-256, then sends its exact ALTER, UPDATE,
and ANALYZE byte slices as three separately committed calls on the same
advisory-lock-owning connection. The brief catalog ALTER can still block a
reader while it holds ACCESS EXCLUSIVE; the long UPDATE takes ROW EXCLUSIVE
and row locks after that ALTER commits, so an ordinary SELECT can proceed.
The tracker writes the unchanged full-file checksum receipt only after all
three calls succeed. An interruption after ALTER or UPDATE leaves no receipt;
the next run repeats the idempotent ALTER and NULL-guarded backfill. Other
migrations retain their existing one-call execution and error wrapping.
Phase start and finish structured logs name the migration path and phase;
the existing lock-wait and completion events retain their meaning.

Read-side proof on the same fixture, `EXPLAIN (ANALYZE, BUFFERS)` of the
final `PackageManifestsQuery`: 865.8 ms first run (cold cache), then 46.1 ms
and 46.6 ms warm, 13,518 shared-hit buffers, no new index. The manifest CTE
still reads once through `content_files_relative_path_trgm_idx`; the tag leg
is an Index Scan on `scope_generations_pkey` at loops=3000, ~0.002 ms per
probe, confirming the pre-change 100k-row shim. The manifest-less leg is a
1 ms anti-join returning zero rows on this fixture (every scope stores a
manifest). The diagnostic per-outcome count on the fixture reads 2970 clean
plus 30 null_tag: the 30 pending-scope manifests stayed NULL through clause
(i), as designed. The `unactivated_tag` outcome fires only when a tag names
a live never-activated generation and is pinned by the live ahead-write
test, not by this fixture.

Before/after on the same fixture shape (review finding 3): the base-revision
query (the #7776 dirty union) runs 19.6 ms warm on a freshly seeded table,
but that table holds manifests in 53 contiguous heap blocks, which flatters
any scan. After the migration UPDATE churns the table (the realistic layout)
plus `VACUUM (ANALYZE)`, base runs 33.2 ms and 33.4 ms warm with 4,173
buffers for 3030 rows, while the new query runs 36.9 ms and 37.7 ms warm
with ~13,000 buffers for 3000 rows: +11%, bounded by the 3000 pkey probes
(~6 ms, partly offset by dropping the MAX aggregate and IN-subquery leg)
and the wider CTE carrying the tag. All buffers are shared-hit; no new
index. The loader runs this read once per code-call materialization pass,
not per key, so a sub-50 ms read keeps its budget while the tag buys the
precision gain and the post-hole permanence the union could not.

Operator visibility: each manifest read counts every row it saw into
`eshu_dp_producer_manifest_tag_outcomes_total` by `kind` (package|gomod) and
`outcome` (clean|null_tag|dangling_tag|unactivated_tag|manifest_less). A
rising non-clean share after deploy means tags are failing safe; the
per-outcome diagnostic above distinguishes legacy NULLs (converging as
generations rewrite) from live unactivated tags (real ahead-write
pressure). Post-deploy SQL: wrap either manifest query as
`SELECT outcome, count(*) FROM (<query>) AS q(scope_id, content, outcome)
GROUP BY 1 ORDER BY 1;`.

Benchmark Evidence (#7760): baseline is the base-revision manifest query on
the 1M-row / 3000-scope fixture (local PostgreSQL 18, post-VACUUM churned
state): 33.2–33.4 ms warm, 4,173 shared-hit buffers, 3030 rows. After: the
tag-rule query on the identical storage state, 36.9–37.7 ms warm, ~13,000
shared-hit buffers, 3000 rows (+11%, bounded by 3000
scope_generations_pkey probes at ~0.002 ms each, partly offset by dropping
the MAX aggregate and IN-subquery leg; no new index). The loader runs this
read once per code-call materialization pass, not per key, so the sub-50 ms
read keeps its budget. Migration: ADD COLUMN 4.0 ms (catalog-only),
backfill UPDATE 30.9 s over 1M rows (890,300 tagged, 109,700 skipped with
counts cross-checked), ANALYZE 1.0 s. These are per-statement timings;
the original one-message execution retained the ALTER lock across the backfill.

No-Regression Evidence (#7033 migration 169 execution): the phased executor
keeps the same SQL bytes and full-file checksum and moves only the transaction
boundary. The migration-specific tracked live proof covers a failed backfill
and receipt-free retry, concurrent read and legacy NULL-tag write during the
backfill, ownership exclusion, completed-receipt replay, and a bounded 55P03
retry of only the UPDATE phase.

Observability Evidence (#7760): `eshu_dp_producer_manifest_tag_outcomes_total`
counts every manifest row by kind (package|gomod) and outcome
(clean|null_tag|dangling_tag|unactivated_tag|manifest_less); a rising
non-clean share means tags are failing safe. Post-deploy per-outcome counts
come from the diagnostic query above; coverage row and metrics reference
updated in the same PR.

Observability Evidence (#7033 migration 169 execution):
`bootstrap.postgres.migration.phase_applying` and `phase_applied` structured
events expose `path`, `phase`, and completed-phase `duration_ms`; existing
`migration.lock_wait`, `migration.recorded`, and `migrations.complete` events
show bounded contention and final receipt/progress. These are bootstrap logs,
not a new high-cardinality metric.

## Production Index Set Measurement (#7961)

#7961 reported a 96.3 s blocked read during migration 169 on a fixture with
the production `content_files` index set. That run used an image built before
#7946, which sent the whole file as one message. From #7946 onward, the
tracked migrator runs 169 in three committed phases. This section measures
that phased path on the same fixture shape.

Fixture: local PostgreSQL 18.6, `shared_buffers` 1 GB, `max_wal_size`
400 MB, `full_page_writes` on, `wal_compression` off. Migrations 001-168 are
applied through the tracked migrator. `content_files` carries all nine
indexes (two trigram GIN) and both secret-lines triggers. The data is
synthetic: 782 repository scopes and 157,422 rows of about 320 identifier
tokens each. Every row is eligible for the backfill. Sizes: heap 28 MB,
TOAST 622 MB, indexes 524 MB.

| Execution | ALTER lock hold | Backfill | ANALYZE | WAL | Longest read wait |
|---|---|---|---|---|---|
| One message, pre-#7946 image (#7961 report) | whole run | 99.3 s total | in the same run | 8.35 GB | 96.3 s |
| One message, control in the live test (run 1) | whole run | not measured | in the same run | not measured | 100.9 s |
| Phased, tracked migrator (live test, run 1) | 0.61 ms | 102.1 s | 0.29 s | 9.10 GB | 48 ms over 1,021 reads |
| Phased, tracked migrator (live test, run 2) | 0.72 ms | 140.8 s | 0.39 s | 9.10 GB | 10 ms over 1,412 reads |
| Phased, psql phase slices | 32 ms | 101.1 s | 0.39 s | 9.06 GB | 89 ms over 625 reads |

The second run's control blocked a read for 117.3 s. Its slower backfill
reflects other load on the shared laptop during that run (load average
about 13), not a change in the work. The reader loop ran one
`content_files` read every 100 ms across all three phases, and a pg_locks
sampler ran at the same rate. Lock sequence on `content_files`:

1. `add_column`: AccessExclusiveLock, catalog-only, committed in under 1 ms.
   It waits only for in-flight readers. The migrator's `lock_timeout` and
   retry budget bound that wait.
2. `backfill`: the UPDATE holds RowExclusiveLock and row locks. The sampler
   saw no AccessExclusiveLock in this phase. The only other modes seen were
   readers (AccessShareLock) and autovacuum (ShareUpdateExclusiveLock).
3. `analyze`: ShareUpdateExclusiveLock, which blocks neither reads nor writes.
   ANALYZE stays in its own phase: it costs 0.29 s, and a separate migration
   would add a ledger row for no lock benefit.

The receipt-skip and receipt-free replays are idempotent. After each apply,
the ledger holds exactly one 169 receipt. The receipt-free replay left every
`content_files` ctid unchanged, so it rewrote no row (the
`generation_id IS NULL` guard). It took 69 ms in run 1 and 78 ms in run 2.
Interruption at this scale (run 2): the test terminated the migrator's backend
during the backfill. The committed ALTER stayed, no row was tagged, and no
receipt was written. A rerun completed with exactly one receipt. #7946's
`migration_169_interruption_live_test.go` covers cancel and terminate on a
one-row table; this run repeats terminate on the production index set.

The 169 header is imprecise and cannot be edited, because shipped migrations
are immutable (#7002) and #7946 pins its checksum. The header says the
backfill "takes row locks only" and is "online-safe". That holds only when
the Eshu migrator applies the file. A tool that sends the whole file as one
simple query holds AccessExclusiveLock through the backfill (control row
above). Its figure of 30.9 s per 1,000,000 rows came from a fixture without
the production index set (#7961). With that index set, the backfill costs
about 0.65 ms (run 1) to 0.89 ms (run 2) and 58 KB of WAL per row.

Heap-scaled data point (2026-10-11, the same tracked migrator, same row
count, rows shaped for a larger heap): heap 307 MB, TOAST 112 kB, indexes
472 MB. The backfill took 95.7 s and wrote 6.03 GB of WAL. ANALYZE took 2.1 s.
Over 8,503 pg_locks samples, no AccessExclusiveLock appeared. The longest
read wait was 481 ms over 967 reads. A replay with every row tagged took
0.58 s. A backfill cancelled at 20 s by statement timeout left 0 tags and 0
receipts, and its rerun recorded one receipt. In that rerun, add_column took
1,002 ms and a read waited 907 ms. An autovacuum worker on `content_files`
holds a lock that conflicts with the ALTER. An ordinary autovacuum yields
only after `deadlock_timeout` (1 s by default); an anti-wraparound autovacuum
does not yield, so the ALTER waits for its 5 s `lock_timeout`. Readers queue
behind the waiting ALTER in both cases. The live test vacuums the seeded table first, so its 1 s bound measures
the phases, not that wait. The deployment note in the migrations README
records the hazard.

Limits: the text is synthetic, the disk is a laptop NVMe, and there is no
streaming replica. The live test kills the backend
(`pg_terminate_backend`); the heap-scaled run cancels by statement timeout.

The phased run removes the read block, but the WAL volume does not change. A
streaming standby replays about 9.1 GB for this fixture. Replica replay lag
was NOT_CHECKED.

No-Regression Evidence (#7961): no SQL or executor change. The live test
`TestMigration169PhasedBackfillReaderBoundLive` fails when any read waits
over 1 s. With the tracked apply forced to one message (seeded violation,
not committed), it failed: a read waited 4.49 s at 8,000 rows. On the
unchanged tree it passes at 8,000 rows (about 25 s of test time: 22.3 s to
29.7 s over three runs, host load varied) and at 157,422
rows (582 s, every step including interruption and control). Rerun at scale with
`-args -eshu.migration169.rows=157422`.

No-Observability-Change (#7961): operators see schema application through the
existing `bootstrap.postgres.migration.phase_applying` and `phase_applied`
events (`path`, `phase`, `duration_ms`). The `migration.lock_wait`,
`migration.recorded`, and `migrations.complete` events are unchanged.
