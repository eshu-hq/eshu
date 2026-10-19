# Narrow the Manifest-Less Dirty Rule (#8243)

The three producer manifest reads (`PackageManifestsQuery`,
`ComposerManifestsQuery`, `GoModuleManifestsQuery` in
`go/internal/storage/postgres/code/producers`) end with a manifest-less leg.
It returns a dirty row for a repository scope that has an active generation,
no stored manifest of the read's kind, and a generation that never activated.
The leg is the only signal for the supersede-then-delta hole (#7609): a delta
deletes a manifest from `content_files`, Ack refuses it (superseded,
`activated_at` NULL), and the next delta never touches the path. Per-row
generation tags (#7760) cannot see a row that was deleted.

Before this change every generation with `activated_at` NULL counted. On the
QA environment 9,303 superseded or failed generations that never activated
(all with a NULL write-start marker) kept 512 of 812 repository scopes dirty
for the npm read, 739 for composer, and 784 for go.mod. The anchored
definition scan then read about 118,000 active file facts per package leg.

## The rule

`ManifestLessLeg` (`dirty_rule.go`) is now one constant that all three reads
end with. A generation with `activated_at` NULL keeps the scope dirty unless:

```sql
pending.status IN ('superseded', 'failed')
AND COALESCE(COALESCE(pending.projection_write_started_at, '-infinity'::timestamptz) <= last_full.pws, false)
```

`last_full` is a `LEFT JOIN LATERAL` whose body is the `last_full` CTE of
`uncoveredProjectionWritersQuery` byte for byte, with `$1` bound to the outer
scope: the newest generation with `is_delta = false` and `activated_at` set,
by `ingested_at DESC, generation_id DESC`, read for its write-start marker
(`pws`). In practice:

- every other row with `activated_at` NULL stays dirty: pending, completed,
  or an active row without `activated_at`;
- a newest full with a NULL marker covers nothing;
- a scope with no activated full stays dirty;
- the `COALESCE(..., false)` wrapper keeps a NULL comparison dirty.

This rule is more conservative than the collector's graph-truth probe.
`uncoveredProjectionWritersQuery` filters `projection_write_started_at IS NOT
NULL`, so it treats every NULL-marker superseded or failed row as clean. This
leg keeps such a row dirty unless the newest activated full carries a marker.

## Why it is exact

- A full generation upserts every content Record. So after a full that
  started writing after a generation's last write, `content_files` reflects
  that full for every path. If the scope has no manifest row, the full's
  truth has no manifest either.
- For a generation with a marker, the comparison is the exact complement of
  `UncoveredProjectionWriters`. Both retention statements
  (`generationRetentionCandidateQuery`,
  `generationRetentionTargetedCandidateQuery`) use the same definition.
- A NULL marker means the generation never wrote under post-149 code, or
  wrote under pre-149 code:
  - every projection loop sets the marker after `LoadFacts` and before
    `Runner.Project` (`Service.processWork` calls `markProjectionWriteStarted`;
    `drainProjectorWorkItem` in `cmd/bootstrap-index` calls
    `markBootstrapProjectionWriteStarted`; the ingester runs
    `projector.Service`);
  - a missing marker returns `errWriteMarkerMissing`;
  - a replayed failed generation gets its marker before it writes.
  A full with a marker was written in the marker era, after any pre-149
  write, so it covers every NULL-marker row.
- A full with a NULL marker was written before migration 149, so its write
  time is unknown. Ordering by `ingested_at` is not sound for it: before
  migration 149 the heartbeat could supersede a generation in the middle of
  its write and release the scope guard (#7389).

## Deviation from the earlier ruling

The first arbiter ruling proposed three exemptions:

- (b) the uncovered-writer complement;
- (c1) never claimed: the projector work row has `last_attempt_at` NULL;
- (c2) a later activated full whose `COALESCE(marker, ingested_at)` is past
  `superseded_at` plus one hour.

The divergence ruling chose the rule above. (c1) and (c2) are not adopted, for
these reasons:

- The ruling's (c2) is unsound without `last_attempt_at <= superseded_at`.
  Before #7266 (2026-09-26) a superseded generation could be replayed and
  claimed after `superseded_at`, so it could write long after that time.
- The corrected (c2), and the strict union of both rules, need a
  `fact_work_items` probe for every never-activated row. On the QA replica
  that cost +110 to +175 ms per manifest read (warm) for little more
  saving: on 2026-10-16 its union dirty set held 18 scopes and 11,940 file
  facts, against this rule's 25 and 12,129. It also couples a hot read to
  the queue table.
- (c1) never-claimed is redundant. This rule already exempts a never-claimed
  row through its NULL marker whenever the newest full has a marker. It
  remains an optional later refinement for scopes whose newest full is
  unmarked. Its census:
  - every `ContentWriter.Write` sits behind a `ProjectorQueue.Claim`;
  - that claim has stamped `last_attempt_at` (the `claimed` CTE of
    `claimProjectorWorkPoolSQL`) continuously since 2026-05-05;
  - on QA, 9,303 rows: 5,382 never claimed, 0 without a work row.

The two rules differ in both directions, and the live tests carry both
directions as labelled fixtures.

- This rule keeps more: a scope whose newest activated full has no marker
  (`legacy-row-older-than-unmarked-full`). The earlier (c2) clears it through
  `ingested_at`.
- The earlier rule keeps more, and this rule clears, three fixtures:
  - `divergence-4a-claimed-unmarked-row-before-marked-full`: a claimed
    NULL-marker row superseded 30 minutes before a marked full;
  - `divergence-4b-claimed-failed-unmarked-row-marked-full`: a claimed failed
    NULL-marker row with a marked full;
  - `late-claim-914`: a NULL-marker row claimed after its supersede and after
    the marked newest full.
  The era argument above covers all three: a claim that wrote after a marked
  full existed would have set the marker first. On the stage-1 shim, which
  planted these as if they had deleted a manifest, the narrowed loader
  returned 9 facts where the previous rule returned 13.

A one-time QA census (2026-10-16 13:16Z, read-only, not a read-time conjunct)
counted the superseded or failed NULL-marker generations whose projector work
row has `last_attempt_at` after their scope's newest activated full's write
start. The result was 0 rows in 0 scopes. 9,013 such rows have a marked
newest full and 290 do not. The census statement (counts only):

```sql
SELECT count(*) FILTER (WHERE w.last_attempt_at > lf.pws) AS rows,
       count(DISTINCT g.scope_id) FILTER (WHERE w.last_attempt_at > lf.pws) AS scopes
FROM scope_generations AS g
JOIN ingestion_scopes AS s
  ON s.scope_id = g.scope_id AND s.scope_kind = 'repository'
LEFT JOIN LATERAL (
    SELECT projection_write_started_at AS pws
    FROM scope_generations
    WHERE scope_id = g.scope_id
      AND is_delta = false
      AND activated_at IS NOT NULL
    ORDER BY ingested_at DESC, generation_id DESC
    LIMIT 1
) AS lf ON true
LEFT JOIN fact_work_items AS w
  ON w.work_item_id = 'projector_' || g.scope_id || '_' || g.generation_id
WHERE g.status IN ('superseded', 'failed')
  AND g.activated_at IS NULL
  AND g.projection_write_started_at IS NULL;
```

## Proof

All tests run on real PostgreSQL 18 through the real loader
(`FactStore.LoadActiveCodeCallSymbolDefinitionFacts`):

- `TestReducerContentionGateManifestLessDirtyRuleKeepsEveryScopeThatCanNeedAProducer`
  covers 24 lifecycle fixtures, each asserted present or absent in the
  producer set (`$5` of the anchored scan) for the package.json,
  composer.json, and go.mod reads. These include every case of ruling C3:
  - an unmarked legacy row with a marked full;
  - unmarked and missing fulls;
  - marked writers above, equal to, and below the full;
  - pending rows with and without a marker;
  - failed rows;
  - the deleted-manifest hole, and the hole healed by a later marked full;
  - a refused never-activated full writer under an active marked full,
    which turns RED if the `last_full` lookup drops
    `activated_at IS NOT NULL`;
  - completed and NULL-`activated_at` anomalies;
  - the three divergence fixtures.
  On `origin/main` it failed for 9 fixtures per read (each kept dirty where
  the new rule clears it).
- `TestReducerContentionGateManifestLessDirtyRuleMatchesUncoveredProjectionWriters`:
  for the 10 fixtures with only marked superseded or failed rows, the scope
  is dirty exactly when the probe reports a writer. On `origin/main` it failed
  4 of 10.
- `TestReducerContentionGateManifestLessDirtyRuleSeededRedGreen` runs per
  read:
  - the previous rule keeps the covered legacy row (RED);
  - a rule without the `COALESCE(..., false)` wrapper drops the
    unmarked-full and no-full scopes (RED);
  - the shipped rule gets both right (GREEN).
- `TestReducerContentionGateManifestLessDirtyRuleNaiveRuleLosesTheHole`: the
  naive `status <> 'superseded'` rule loses the hole scope.
- `TestReducerContentionGateManifestLessDirtyRuleReturnsTheSameDefinitions`:
  identical fact ids, and so the same `symbol_definition_fact_count`, under
  the previous leg and the new one, with fewer producer scopes bound. The
  previous leg is derived from the shipped constant by cutting the two added
  spans (`TestManifestLessLegDerivesThePreviousRule`).
- Lockstep, in the parent package's test code:
  - `TestManifestLessLegLastFullIsTheProbeLastFull` pins the LATERAL body to
    the probe's `last_full` CTE byte for byte, and pins both comparisons;
  - `TestUncoveredWriterDefinitionsAgree` pins all four copies' selector and
    comparison;
  - seeded mutations turned them RED: an extra probe `last_full` predicate,
    the probe's ORDER BY, retention's `>`, targeted retention's `is_delta`,
    the leg's `<=`, the leg's wrapper, and the leg dropping
    `activated_at IS NOT NULL` from `last_full` (which also turns the
    refused-full-writer fixture RED in all three reads).
- Hermetic: `TestManifestReadsEndWithTheSharedManifestLessLeg`,
  `TestManifestLessLegKeepsEveryUnactivatedGenerationExceptCoveredWriters`,
  and the existing `TestPackageScopeIDsIncludesNullDirtyScopes`.

Performance Evidence: `EXPLAIN (ANALYZE, BUFFERS)` on the QA environment's
streaming replica, 2026-10-16 13:17Z, warm, three runs each. The statement
text is dumped from the built constants, and the anchored scan is the shipped
`listAnchoredActiveCodeCallSymbolDefinitionFactsQuery` with an unpublished
key, so the producer set is the dirty set. Measured before #8223 added the
composer fixture-directory filter; the composer rows reflect the pre-#8223
statement.

| Statement | Before | After |
|---|---|---|
| npm manifest read | 40.0-40.8 ms, 19,845 buffers | 50.5-53.5 ms, 27,620 |
| composer manifest read | 43.2-46.8 ms, 22,021 | 60.7-61.0 ms, 33,815 |
| go.mod manifest read | 27.6-29.2 ms, 22,312 | 46.3-48.3 ms, 34,720 |
| Dirty scopes (npm / composer / go.mod) | 512 / 739 / 784 | 25 / 26 / 25 |
| Union dirty set (npm + composer), counted a minute later | 771 scopes, 117,666 file facts | 25 scopes, 12,129 file facts |
| Anchored scan, npm set | 4.19-4.79 s | 0.79-0.82 s |
| Anchored scan, union set | 6.11-6.56 s | 0.79-0.87 s |

Row-set differential against the shipped statements on the same replica:

- Rows in the narrowed read that are not in the shipped read: 0 for each
  read.
- Rows outside the manifest-less leg: identical in both directions.
- Every dropped scope's never-activated rows satisfy the exemption: 5,511 of
  5,511 rows (npm), 8,318 of 8,318 (composer), 8,951 of 8,951 (go.mod).

The per-scope `last_full` probe is index-backed: an `Index Scan using
scope_generations_scope_latest_lookup_idx` in each plan, 813 loops for the
composer and go.mod reads and 519 for npm.

- All remaining scopes have a newest activated full without a marker.
- The package leg per load (manifest reads plus scan) goes from about 4.2 to
  6.6 s to about 0.85 s.
- Real loader on the stage-1 QA-shaped shim (127,172 file facts, 9,721
  never-activated rows), three runs:
  - `package_leg_duration_seconds` 10.39 (cold), 3.42 and 3.55 s at 776
    producer scopes, then 0.21, 0.16 and 0.17 s at 39;
  - `go_leg_duration_seconds` 5.06 (cold), 3.28 and 3.30 s at 769 scopes,
    then 0.10 to 0.11 s at 32.
- Two shapes were measured and rejected:
  - a correlated subquery per never-activated row: 8,750 subplan loops,
    42 to 67 ms on the shim against 5 to 9 ms;
  - a MATERIALIZED per-scope CTE: the planner misestimated it into a nested
    loop (834 x 813 rows), 122 to 189 ms on the replica.

No-Observability-Change: the change adds no metric, span, or log field and
removes none. Existing signals already show the change:

- The loader's `code call symbol definition load` log line carries
  `package_producer_scope_count` and `go_producer_scope_count`, which expose
  the dirty-set size, plus `package_leg_duration_seconds` and
  `go_leg_duration_seconds`. Expect the scope counts to fall from about 771
  to about 25 on QA, and the package leg to fall below a second.
- `eshu_dp_producer_manifest_tag_outcomes_total{outcome="manifest_less"}`
  falls in the same proportion.

## Concurrency

The reads stay plain autocommit SELECT statements. `pg_locks` inside a
transaction shows only AccessShareLock on `content_files`,
`scope_generations`, `ingestion_scopes` and their indexes. No claim, lease,
Ack, or write path changes. Read Committed gives one snapshot for the whole
statement. The marker commits before the content write, so a snapshot that
sees the write also sees the marker.

## Deployed re-measurement (2026-10-19)

Deployed QA image `sha-aefdc43b` contains #8272 (resolution-engine pods up
14 h). Read-only rerun on `eshu-postgresql-read`. The manifest reads are the
shipped constants dumped from Go (`PackageManifestsQuery`,
`ComposerManifestsQuery`, `GoModuleManifestsQuery`); the old-rule variant is
verified byte-identical (modulo whitespace) to the pre-#8272 predicate taken
from `938661036^`. The anchored scan is the shipped
`listAnchoredActiveCodeCallSymbolDefinitionFactsQuery` with an unpublished
key, `NULL` cursor, limit 500, and the measured union scope set.

| Statement | Deployed, warm, 3 runs | Pre-change (2026-10-16 replica) |
|---|---|---|
| npm manifest read | 30.4–31.3 ms | 40.0–40.8 ms |
| composer manifest read | 33.8–34.8 ms | 43.2–46.8 ms |
| go.mod manifest read | 19.6–21.3 ms | 27.6–29.2 ms |
| Dirty scopes (npm / composer / go.mod), new rule | 0 / 0 / 0 | 512 / 739 / 784 |
| Dirty scopes, old rule on the same data | 0 / 0 / 0 | — |
| Union producer scopes | 328, all manifest-bearing | 771 (25 post-fix) |
| Anchored scan over the union set | 14.0 s cold, 5.0 s warm ×2 | 6.11–6.56 s, 0.79–0.87 s post-fix |

The fleet moved under the measurement: 788 repository scopes, 1,691
`scope_generations` rows, oldest ingested 2026-10-16 20:36Z. The 9,700 stale
never-activated generations from the issue are gone (oldest-first removal,
consistent with generation retention; the retaining mechanism was not
verified in this drive). The one remaining never-activated row is
`eshu:global:genesis`, a non-repository scope the leg never visits. With no
stale rows the old rule returns the same empty dirty set on this data, so a
before/after gap is no longer reproducible on QA; the Proof differential above
(0 rows outside the shipped reads; shim loader 3.42–3.55 s to 0.16–0.17 s)
stands as the fix evidence. Today's ~5 s scan covers the 328 manifest-bearing
scopes (~191,000 file facts), which the leg visits by design: cost now scales
with scopes that hold manifests, not with the age of generation history.

No `code call symbol definition load` line fired on QA in the sampled window
(about 9,400 ingester lines per hour with zero `code call` mentions;
resolution-engine, API, and MCP logs likewise empty), so production
`package_leg_duration_seconds` per load under the deployed image is still
unobserved. A second lane closed the gap read-only the same day: the real
loader (`FactStore.LoadActiveCodeCallSymbolDefinitionFacts` built from HEAD;
loader SQL byte-identical to the deployed image, comment-only drift since)
against `eshu-postgresql-read` with `default_transaction_read_only=on`,
statement texts dumped from the shipped constants. Its own log line reports,
3 runs each: an unpublished npm key (`package:qa-8243-unpublished-probe#NoSuchExport`)
`package_producer_scope_count=0` at 34–45 ms per load with 0 facts; a
published key (`package:boattrader#Qa8243ProbeExport`, first npm manifest in
the read) 1 producer scope at 103–123 ms per load with 0 facts. The EXPLAIN
leg cost above (about 5.1 s reads plus scan over the 328-scope manifest set)
remains the replica record for a wide key.

## Residuals

The three below are accepted by the divergence ruling, each inherited from
its source.

- A zombie writer in the migration-149 rollout window: a pre-149 pod still
  writing after a post-149 pod's marked full. Migration 149 landed
  2026-09-30, and #7266 on 2026-09-26. NOT_CHECKED; the C4 census found 0
  claims after a marked full on QA.
- A zombie writer after lease loss (#7389): an expired-lease attempt whose
  writes land after a later full started.
- Cross-host clock skew on the app-clock marker (#7389).

Not covered by the ruling, recorded for completeness:

- RetainedPaths: a covering full whose manifest body could not be re-read
  keeps the path without re-upserting it (`buildProjection` sets
  `RetainedPaths` from `snapshotFilePaths`). A manifest that a covered
  generation deleted would then stay missing. Second-order and not fixtured.
- Pre-existing and unchanged: a repository with several manifests that loses
  one to a refused delta never reaches the manifest-less leg.

NOT_CHECKED:

- cold-cache replica runs;
- production `package_leg_duration_seconds` log lines (no code-call
  materialization loads in 24h of resolution-engine logs on 2026-10-19; the
  per-load numbers above are the real loader measured read-only against the
  replica).
