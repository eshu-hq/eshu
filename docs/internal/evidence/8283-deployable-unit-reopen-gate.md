# Deployable-Unit Reopen Changed-Evidence Gate

Issue #8283. Every deferred-maintenance pass re-opened every succeeded
`deployable_unit_correlation` item on its scope's replay floor, and in steady
state almost every replay reproduced the same graph. A succeeded item is now
re-opened only when something it read can have changed.

## Decision record

- **Build order.** The #8283 design comment of 2026-10-16 22:11Z replaced the
  #8082 close-comment order (readiness-aware reopen first). The
  changed-evidence gate is built alone, for `deployable_unit_correlation`
  only. A listing filter on the readiness gates is rejected: the fence and
  canonical gates are corpus-wide, and the same transaction re-opens
  `deployment_mapping` first. The readiness-aware reopen goes to a separate
  issue, opened only if the deferral share stays above 25 % with this gate
  deployed.
- **Fingerprint basis.** An arbiter ruling amended the 22:11Z generation-id
  basis. The fingerprint is a digest of the sorted `resolved_id` set the
  handler's fused read selects. A generation-id basis misses rows appended
  under an already-active generation. A re-run `deployment_mapping` writes
  into the relationship generation of its scope generation (same id) through
  `insertResolvedSQL` (`ON CONFLICT DO NOTHING`), and
  `activateResolutionGenerationSQL` (`ON CONFLICT DO UPDATE SET status =
  'active'`) leaves that id in place.
- **Digest.** The ruling named md5. The repo gate
  `TestProductionSQLHasNoMD5Calls` (#6753) rejects md5 in production SQL,
  because OpenSSL FIPS rejects it. The digest is
  `encode(sha256(convert_to(...)), 'hex')`, the same form as
  `changed_since_sql.go`.
- **Same-transaction hazard: option (i), a one-pass bound.** A
  `deployment_mapping` re-opened in the same transaction appends its rows
  after the gate ran, so the next pass re-opens an item that changed that
  way. Today's unconditional reopen has the same window whenever the item
  runs first. A pass runs only after a drain that committed something
  (`RunDeferredRelationshipMaintenanceAfterShardDrain`). If no later pass
  runs, the item keeps its old graph until one does. The exact
  completion-driven wake is a follow-up.

## Shape

- **Side table.** Migration 188 adds `deployable_unit_reopen_fingerprints`:
  one row per succeeded item with `repo_ids`, `input_fingerprint`,
  `logic_version`, `claimed_at` and `recorded_at`. It has a cascading FK to
  `fact_work_items`, and the PK on `work_item_id` serves the cascade.
- **Fused read.** `GetFingerprintedResolvedRelationshipsForReposWithCorpusFence`
  is derived from `listResolvedByReposWithCorpusFenceSQL` by markers. Its
  by-repos rows move into a MATERIALIZED `input_rows` CTE that also carries
  `resolved_id`. The fingerprint is `backfill.ResolvedInputDigestSQL` over
  those same rows, in the same statement snapshot.
- **Success write.** The handler records the read-time value after every
  graph write. The write is fenced on the exact claim (`work_item_id`,
  `last_attempt_at`, a live `claim_until`, `FOR NO KEY UPDATE`) and keeps the
  newest claim.
- **Gated listing.** `backfill.ListDeployableUnitReopenCandidatesQuery` is
  the floored listing plus a LEFT JOIN on the fingerprint table and an
  `input_changed` column. It re-opens an item when:
  - the fingerprint row is missing;
  - the logic version moved;
  - a recompute through `ResolvedInputFingerprintSQL` differs.
- **Where the gate applies.** The whole pass, the #7584 partition-scoped pass
  (through a derived listing) and `eshu-bootstrap-index` all use the gated
  listing. The other three correlation domains are unchanged.
- **Zero candidates.** A zero-candidate item records `'{}'` and the
  `empty-resolved-input` sentinel, so a change in resolved input never
  re-opens it.

## Measurements

QA environment, read-only, while it was re-ingesting from scratch. These are
not a deployed before and after.

- **QA wave 2026-10-16 23:17Z.** 786 items re-opened.
  `eshu_dp_reducer_executions_total{deployable_unit_correlation,status=deferred}`
  stayed at 2785 for the whole wave, so there were 0 deferrals. The
  run-duration sum went from 1186.9 s to 1365.9 s (about 179 handler-seconds),
  over about 3 minutes of wall time (23:17:00 to 23:19:54).
- **The 2785 lifetime deferrals** came from the initial ingest. Every
  relationship generation was created between 22:10Z and 22:20Z, and none
  since, although two waves each re-opened 55 `deployment_mapping` items.
- **Readiness filter.** It would have skipped 0 of 786 on QA. The ruling's
  production figure is 0 of 799.

Performance Evidence: local PostgreSQL 18.6, on the #6740 seed (5000 active
scopes, 10000 relationship generations, 200000 resolved rows, 20000 succeeded
work items). Each run used 10 repository ids, jit off, three warmups and nine
paired runs.

| Statement | Execution (median, range) | Shared hit |
| --- | --- | --- |
| shipped fused read, fence complete | 5.419 ms (5.390-5.483) | 660 |
| fingerprinted fused read, fence complete | 5.624 ms (5.587-5.708) | 660 |
| shipped fused read, fence incomplete | 3.746 ms (3.744-3.752) | 580 |
| fingerprinted fused read, fence incomplete | 3.933 ms (3.927-3.943) | 580 |

- **Cost of the fingerprint.** +0.205 ms with the fence complete and
  +0.187 ms with it incomplete. It was slower in 9 of 9 pairs each, with
  identical buffers.
- **Rejected InitPlan form.** It re-read the rows: 6.889 ms and 1038 buffers
  (+1.25 ms, +378).
- **Plan.** The by-repos access path is kept: a BitmapOr over
  `resolved_relationships_source_repo_idx` and `_target_repo_idx`,
  hash-joined to `relationship_generations`, now feeding the CTE.

Listing, same seed. Every item has a fingerprint row, and none changed:

| Listing | 5000 items | 786 items (QA scale) |
| --- | --- | --- |
| ungated (`ListSucceededReducerWorkItemsByDomainQuery`) | 35.7 ms, 10361 hit | 15.4 ms, 10361 hit |
| gated (`ListDeployableUnitReopenCandidatesQuery`) | 259.2 ms, 219378 hit | 50.3 ms, 43439 hit |

- **Per item.** The fingerprint SubPlan costs 0.039 ms, and the hash on
  `relationship_generations` is built once. The gated listing returned
  `input_changed = false` for 5000 of 5000.
- **Trade at QA scale.** +34.9 ms of listing replaces 786 reopen round trips
  plus 786 reducer executions (about 179 handler-seconds in the QA wave).
- **Trade at 5000 items.** +223.5 ms replaces 5000 of each.

Write costs:

- **Success write.** One claim-fenced upsert per success, 0.134-0.143 ms each.
  800 upserts took 0.20 s of wall time in one session. In steady state the
  gate stops the re-runs, so most waves write almost none.
- **Retention cascade.** The cascading FK adds +15.9 to +17.5 ms per 5000
  cascaded work items (8 paired runs: 47.2-48.4 ms without the table,
  63.6-65.5 ms with it). That is about 3.3 µs per deleted work item, about
  56 µs per pruned generation at QA's 17.2 work items per generation, and
  +35 % on the work-item cascade step.
- **Retention row count.** `generationRetentionRowCountsQuery` does not count
  the new grandchild, so it under-counts by at most one row per pruned item.
  This is a follow-up.

## Concurrency

The gate takes no lock. It reads the fingerprint table, and the reopen UPDATE
is the same per-row `ReopenSucceeded` as before. The success write locks the
one work-item row, then the fingerprint row. A retention cascade takes them in
the same order, so the two cannot close a cycle.

Live proofs in `go/internal/storage/postgres` (isolated schema, PostgreSQL
18.6), also run under `-race`:

- **Concurrent passes.** Four maintenance passes on separate connections
  finished with no error and the exact changed/unchanged split.
- **Write versus reclaim.** In 40 rounds, a stale claim's write ran before,
  after and concurrently with a reclaim plus the fresh write. Every round
  ended on the fresh claim's row, and the fresh write was never rejected.
- **Write versus cascade.** In 40 rounds of a write racing the retention
  cascade, there were 0 deadlocks and no orphan row.

## Mutations

Each mutation was applied to production SQL, run, then restored:

| Mutation | Tests that fail |
| --- | --- |
| skip whenever a row exists | R1, R2, R4, R5, new matching scope, G3, G2, both C2 cases |
| missing row skips | R3 |
| generation-id basis | R2, G3, C2 rows appended, G2 |
| ignore logic version | R4 |
| recompute at write time | both C2 cases (stored value is not the read-time value) |
| drop the claim fence | C1 stale-claim write |
| drop the monotonic guard | older-token write |
| drop both guards | the reclaim race |
| listing drops the target match | D1 differential |
| targeted pass ungated | targeted-pass gate |

On the ungated tree, G1 failed with
`duc-a status after maintenance = "pending", want "succeeded"` and
`eshu_dp_correlation_reopen_skipped_total{deployable_unit_correlation} = 0, want 2`.

Observability Evidence: the gate adds three signals.
`eshu_dp_correlation_reopen_skipped_total{domain,reason="input_unchanged"}`
counts skips on the whole pass. `skipped_unchanged` is added to the
`relationship.backfill.reopen` block span and log line (whole and
partition-scoped passes) and to the `reducer_work_items_reopened` line.
`eshu_dp_correlation_reopened_total` keeps its meaning: items actually
re-opened. A wave whose skipped count equals its candidate count saw no
changed input. A rejected success write fails the run with
`ErrExecutionClaimRejected`, on the fenced-claim path the reducer service
already handles.

## NOT_CHECKED

- **Deployed before and after.** Wave CPU-seconds, wall time,
  skipped-unchanged per wave, `eshu_dp_correlation_reopened_total` per domain,
  and the forced-change canary on a deployed corpus all wait on a deployed
  image.
- **Generation-id over-admit rate.** Not measured on deployed data; the ruling
  estimates about 80 of 799. The row-set basis also admits an item when an
  appended row does not change its edges.
- **Golden-corpus gate.** Not run against the gate. If it shows the
  deployable-unit edge set changed, the completion-driven wake comes into this
  work.
