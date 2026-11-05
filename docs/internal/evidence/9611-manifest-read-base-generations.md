# #9611 Delta Base Pages Bind Candidate Base Generations

The first #9611 change ([9611-manifest-read-name-probe.md](9611-manifest-read-name-probe.md))
made every manifest page probe each requested (package manager, folded name)
pair. Its derived base pages still bound the delta-active scope ids
(`fact.scope_id = ANY($6)`). That is a residual Filter on every row a probe
visits, so at 10x the base cursor page read 1.70x main's buffers. The arbiter
ruling on the issue rejected that regression, and rejected keeping the flat
base shape. It directed this change and set proofs A, B, and C below.

## Change

- `deltaOverlayBaseFullPrefix` (`delta_overlay_query.go`) now ends in
  `AND fact.generation_id = ANY($N::text[])`. `generation_id` is the global
  primary key of `scope_generations` and the trailing column of migration 247's
  folded entity index, so the restriction is part of each probe's Index Cond
  (Rows Removed by Filter 0). Every other derived predicate stays, so the
  binding is a prefilter, not a second base rule.
- `deltaOverlayTargetsQuery` gains one column: a JSON array of each
  delta-active scope's activated full generations at
  `COALESCE(NULLIF(TRIM(union_base), ''), NULLIF(TRIM(baseline), ''))`, the
  exact predicate the derived text applies. Binding them narrows nothing.
- `deltaOverlayBaseGenerations` returns the sorted, distinct candidates of every
  target with a baseline, and whether the base scan runs. It runs when any
  target has a baseline, even with no candidate, so the statements per read do
  not depend on the candidates. `resolveDeltaBases` and the counters still cover
  reached scopes only.
- No statement is added: the candidates ride the existing target lookup.

The same binding on the active leg was measured by the arbiter and disproven
(time rises 1.5x to 4x), so the active leg is unchanged.

## Proof

- Red first: in a throwaway worktree at the pre-change head, with the new test
  files, `TestDeriveDeltaOverlayBaseQueryBindsTheScopePlaceholder`,
  `TestDeriveDeltaOverlayBaseQueryKeepsTheManifestText`,
  `TestDeltaOverlayBaseBindingGuardFiresOnTheScopeArray`, and
  `TestManifestDependencyQueriesProbeEachRequestedName` failed (exit 1). The
  remaining new tests failed to compile against the old API. All pass after.
  The binding guard fires on the scope-bound fragment, which is the seeded
  violation.
- `TestDeltaOverlayBaseGenerationsBindsTheCandidates`: sorted, distinct
  candidates; a legacy delta contributes none; a baseline with no candidate
  still scans with an empty array.
- `TestListActivePackageManifestDependencyFactsDeltaAuxReadsUseCustomPlans` (the
  #9991 unit case) asserts `$6` is the candidate generation list.
- Live, own databases: 167 tests pass, among them the nine #9991 delta cases,
  the #10003 set, the parity proof (the same row counts as before), the plan
  check, and the owned-resolver tests.

## Performance Evidence:

Same container, settings, seeds (`lane3_9611_1x`, `_5x`, `_10x`), and method as
the first note: custom plans with literal binds, generic plans by `PREPARE`,
medians of five interleaved main/new runs, md5 of the positional row text.
"Main" is origin/main's flat text with the scope-bound base. "New" is the
probe with the generation-bound base. The host load average was 13 to 54 on 12
cores.

### Proof A, custom plan

| shape | scale | rows | main ms | new ms | main buffers | new buffers | buffers new/main |
| --- | --- | --- | --- | --- | --- | --- | --- |
| npm400_p1 | 1x | 500 | 471.4 | 140.2 | 343,907 | 126,750 | 0.37 |
| npm400_p1 | 5x | 500 | 4,457.1 | 1,204.6 | 1,779,488 | 637,545 | 0.36 |
| npm400_p1 | 10x | 500 | 17,696.0 | 3,086.3 | 3,558,625 | 1,168,345 | 0.33 |
| npm400_mid | 1x | 500 | 147.0 | 82.7 | 145,415 | 59,779 | 0.41 |
| npm400_mid | 5x | 500 | 2,952.1 | 847.6 | 946,280 | 365,268 | 0.39 |
| npm400_mid | 10x | 500 | 3,122.5 | 1,611.2 | 1,259,196 | 642,568 | 0.51 |
| npm400_last | 1x | 100 | 15.6 | 4.3 | 3,502 | 1,733 | 0.49 |
| npm400_last | 5x | 100 | 105.3 | 7.0 | 18,787 | 1,825 | 0.10 |
| npm400_last | 10x | 100 | 46.4 | 9.1 | 14,138 | 2,227 | 0.16 |
| npm3_p1 | 1x | 500 | 264.7 | 16.4 | 254,887 | 13,326 | 0.05 |
| npm3_p1 | 5x | 500 | 5,879.5 | 109.2 | 1,281,542 | 40,374 | 0.03 |
| npm3_p1 | 10x | 500 | 11,477.2 | 140.0 | 2,564,290 | 69,340 | 0.03 |
| pypi_nuget_p1 | 1x | 500 | 92.1 | 32.0 | 100,554 | 27,220 | 0.27 |
| pypi_nuget_p1 | 5x | 500 | 922.2 | 249.8 | 512,853 | 122,531 | 0.24 |
| pypi_nuget_p1 | 10x | 500 | 2,425.1 | 522.5 | 1,027,052 | 237,266 | 0.23 |
| npm_zero | 1x | 0 | 268.3 | 2.9 | 242,894 | 1,206 | 0.00 |
| npm_zero | 5x | 0 | 5,626.8 | 3.0 | 1,215,335 | 1,206 | 0.00 |
| npm_zero | 10x | 0 | 12,223.2 | 3.4 | 2,431,360 | 1,606 | 0.00 |
| maven600_p1 | 1x | 500 | 71.0 | 46.4 | 64,643 | 42,034 | 0.65 |
| maven600_p1 | 5x | 500 | 535.8 | 355.6 | 334,244 | 195,670 | 0.59 |
| maven600_p1 | 10x | 500 | 1,799.9 | 991.2 | 670,697 | 386,318 | 0.58 |
| npm400_base_p1 | 1x | 500 | 179.9 | 44.6 | 266,498 | 13,972 | 0.05 |
| npm400_base_p1 | 5x | 500 | 4,382.9 | 313.5 | 1,333,589 | 66,442 | 0.05 |
| npm400_base_p1 | 10x | 500 | 13,266.6 | 1,116.8 | 2,667,832 | 129,039 | 0.05 |
| npm400_base_mid | 1x | 500 | 100.3 | 32.0 | 105,477 | 10,792 | 0.10 |
| npm400_base_mid | 5x | 500 | 2,112.1 | 309.5 | 648,134 | 62,749 | 0.10 |
| npm400_base_mid | 10x | 500 | 607.4 | 659.3 | 141,901 | 124,557 | 0.88 |
| npm400_base1_p1 | 1x | 36 | 148.6 | 18.5 | 243,512 | 1,790 | 0.01 |
| npm400_base1_p1 | 5x | 39 | 4,053.6 | 31.5 | 1,215,980 | 3,005 | 0.00 |
| npm400_base1_p1 | 10x | 40 | 11,383.1 | 78.6 | 2,432,014 | 5,052 | 0.00 |
| npm400_base4_p1 | 1x | 147 | 167.7 | 20.5 | 245,384 | 2,902 | 0.01 |
| npm400_base4_p1 | 5x | 131 | 4,661.6 | 88.1 | 1,217,681 | 3,926 | 0.00 |
| npm400_base4_p1 | 10x | 147 | 11,663.8 | 224.4 | 2,433,850 | 6,123 | 0.00 |
| maven600_base_p1 | 1x | 318 | 28.4 | 36.8 | 45,952 | 9,010 | 0.20 |
| maven600_base_p1 | 5x | 500 | 398.5 | 342.4 | 229,592 | 22,529 | 0.10 |
| maven600_base_p1 | 10x | 500 | 828.0 | 648.9 | 459,971 | 42,679 | 0.09 |

### Proof A, generic plan

| shape | scale | rows | main ms | new ms | main buffers | new buffers | buffers new/main |
| --- | --- | --- | --- | --- | --- | --- | --- |
| npm400_p1 | 1x | 500 | 1,016.5 | 212.1 | 126,130 | 126,750 | 1.00 |
| npm400_p1 | 5x | 500 | 11,545.3 | 939.0 | 1,779,488 | 637,545 | 0.36 |
| npm400_p1 | 10x | 500 | 37,044.8 | 3,291.5 | 3,558,625 | 1,168,345 | 0.33 |
| npm400_mid | 1x | 500 | 832.0 | 145.5 | 126,130 | 75,772 | 0.60 |
| npm400_mid | 5x | 500 | 11,131.1 | 1,215.6 | 1,571,309 | 433,861 | 0.28 |
| npm400_mid | 10x | 500 | 24,490.8 | 2,511.7 | 3,157,906 | 777,010 | 0.25 |
| npm400_base_p1 | 1x | 500 | 84.3 | 45.5 | 14,214 | 13,972 | 0.98 |
| npm400_base_p1 | 5x | 500 | 13,819.6 | 446.1 | 1,333,589 | 66,442 | 0.05 |
| npm400_base_p1 | 10x | 500 | 46,855.7 | 1,073.8 | 2,667,832 | 129,039 | 0.05 |
| npm400_base_mid | 1x | 500 | 68.5 | 41.4 | 14,214 | 11,263 | 0.79 |
| npm400_base_mid | 5x | 500 | 9,385.3 | 471.1 | 1,273,163 | 63,904 | 0.05 |
| npm400_base_mid | 10x | 500 | 25,628.4 | 937.8 | 2,549,842 | 126,610 | 0.05 |
| npm400_base1_p1 | 1x | 36 | 2.7 | 12.4 | 572 | 1,790 | 3.13 |
| npm400_base1_p1 | 5x | 39 | 4,429.2 | 39.8 | 1,215,980 | 3,005 | 0.00 |
| npm400_base1_p1 | 10x | 40 | 11,518.0 | 79.1 | 2,432,014 | 5,052 | 0.00 |
| npm400_base4_p1 | 1x | 147 | 10.6 | 21.8 | 1,613 | 2,902 | 1.80 |
| npm400_base4_p1 | 5x | 131 | 5,347.1 | 99.2 | 1,217,681 | 3,926 | 0.00 |
| npm400_base4_p1 | 10x | 147 | 11,784.9 | 220.3 | 2,433,850 | 6,123 | 0.00 |
| maven600_base_p1 | 1x | 318 | 50.5 | 40.5 | 23,818 | 9,010 | 0.38 |
| maven600_base_p1 | 5x | 500 | 2,046.1 | 365.1 | 229,592 | 22,529 | 0.10 |
| maven600_base_p1 | 10x | 500 | 5,659.1 | 758.0 | 459,971 | 42,679 | 0.09 |

Result against A:

- Base shapes, custom plan: met everywhere except `maven600_base_p1` at 1x,
  which reads 0.20 of main's buffers but takes 1.29x the time (36.8 against
  28.4 ms). A seven-run rerun gave 73.2 against 60.0 ms, 1.22x. The page runs
  1,800 index searches (three Maven managers by 600 names) where main's cached
  prefix scan is cheap on this small corpus. At 5x it is 0.86x the time.
- `npm400` active page 1 and cursor page: at most 0.5x of main at 1x and 5x.
  At 10x, page 1 is 0.33 of main's buffers and 0.17 of its time and the cursor page reads 0.510 of main's
  buffers in 0.516 of its time, which **misses the 0.5x bar**, as the first
  note recorded.
- Generic plan: within 1.1x of main except `base1` and `base4` at 1x (12.4
  against 2.7 ms and 21.8 against 10.6 ms). Main's generic plan there drives
  from the one or four scopes; the probe's floor is 400 index searches. At 5x
  both are 0.00x main's buffers. The read runs custom plans.
- Every other shape: within 1.1x of main or +2 ms. Rows identical in every
  shape measured.

### Proof B, retention point

Two 1x corpora (`lane3_9611_ret1`, `lane3_9611_ret24`; seed at the end of
this section) model retention depth. Every one of 400 scopes has a superseded
old full and `depth` superseded cumulative deltas. Each delta re-emits the
same first 100 npm rows of one manifest, with the same stable keys, as a
changed lockfile is re-emitted in every retained delta. Each scope then has a
current full. Every 10th scope is delta-active, with one more delta active over
its current full (now superseded) as the base. `observed_at` is interleaved
across scopes over 7 days. Depth 24 holds 1,476,000 fact rows and 10,440
generations; depth 1 holds 556,000 and 1,240. Every cell is the median of
seven interleaved runs. The base cursor page takes its cursor from the base
rows' own order (row 634 of 1,269). A cursor in active-row order falls after
every base row there.

| shape | depth | runs | rows | main ms | new ms | time new/main | main buffers | new buffers | buffers new/main |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| npm400_p1 | 1 | 7 | 500 | 763.2 | 281.9 | 0.37 | 398,170 | 155,937 | 0.392 |
| npm400_p1 | 24 | 7 | 500 | 7,808.9 | 1,417.4 | 0.18 | 1,640,486 | 554,888 | 0.338 |
| npm400_mid | 1 | 7 | 500 | 439.6 | 135.6 | 0.31 | 148,369 | 66,872 | 0.451 |
| npm400_mid | 24 | 7 | 500 | 436.3 | 129.6 | 0.30 | 166,131 | 66,846 | 0.402 |
| npm400_base_p1 | 1 | 7 | 500 | 389.0 | 80.5 | 0.21 | 297,106 | 14,166 | 0.048 |
| npm400_base_p1 | 24 | 7 | 500 | 4,752.8 | 146.9 | 0.03 | 1,272,806 | 15,219 | 0.012 |
| npm400_base_mid | 1 | 7 | 500 | 375.4 | 56.6 | 0.15 | 223,715 | 7,724 | 0.035 |
| npm400_base_mid | 24 | 7 | 500 | 4,994.6 | 150.9 | 0.03 | 1,192,793 | 8,798 | 0.007 |

Result against B: no shape is worse than main. The worst cell, the active
cursor page at depth 1, is 0.451 of main's buffers and 0.31 of its time.
Growth from depth 1 to 24 on the new side: page 1 3.56x buffers (main 4.12x),
the cursor page 1.00x, the base page 1.07x, the base cursor page 1.14x. Page
1 grows because each probe reads the re-emitted delta rows of its name and
rejects them in the joins. The active cursor page at depth 24 is 0.40 of
main's buffers and 0.30 of its time, so it meets the 0.5x ambition bar there.

Seed: `seed_ret.sql` in the lane scratchpad, `psql -v depth=1` and
`-v depth=24` on databases cloned from a migrated template.

## Statement counts

The committed #9991 harness (tag `perf9991_manifest_delta`) on this head:
48 statements for the delta-active npm read (23 active pages, 1 target lookup,
21 base pages, 1 base lookup, 1 probe, 1 inputs), 3 for the delta-active
Maven read, 42 and 2 with no delta-active scope, and 20,000 rows. Those are
the same counts and rows as before #9611. Times ran at load average 54 and are
not compared.

The target lookup's added column, `EXPLAIN ANALYZE` median of three: 5.7 ms
against 1.8 ms on `lane3_9611_10x` (400 delta-active scopes), and 1.4 ms
against 0.8 ms on `lane3_9611_ret24` (40). It runs once per read.

## QA readback (C)

As the first note says: `read_seconds` and `read_pages` by `read` before and
after, and one bounded `EXPLAIN (ANALYZE, BUFFERS)` of a 400-name npm page.
Also, for lane 1, the retained-generation depth of manifest-bearing scopes,
read-only and bounded:

```sql
BEGIN READ ONLY;
SET LOCAL statement_timeout = '30s';
SELECT count(*) AS scopes,
       percentile_disc(ARRAY[0.5, 0.9, 0.99]) WITHIN GROUP (ORDER BY generations) AS p50_p90_p99,
       max(generations) AS max_generations
FROM (
  SELECT generation.scope_id, count(*) AS generations
  FROM scope_generations AS generation
  WHERE EXISTS (
    SELECT 1 FROM fact_records AS fact
    WHERE fact.scope_id = generation.scope_id
      AND fact.generation_id = generation.generation_id
      AND fact.fact_kind = 'content_entity'
      AND fact.source_system = 'git'
      AND fact.payload->>'entity_type' = 'Variable'
      AND fact.payload->'entity_metadata'->>'config_kind' = 'dependency')
  GROUP BY generation.scope_id
) AS per_scope;
ROLLBACK;
```

## Known limits

- The A misses above: `maven600_base_p1` at 1x (1.22x the time over seven
  runs), the generic `base1` and `base4` cells at 1x, and the 10x active
  cursor page against the 0.5x bar.
- The probe reads each requested name's rows in every retained generation, so
  page 1's cost grows with retention depth (3.56x from depth 1 to 24 here),
  though more slowly than main's.
- The 10x corpus over-weights scope count and dependency density against the
  QA gold point (896 repositories, 3.5M fact_records). The retention corpora
  model depth 24 but not a 90-day retention window or the #7802 chain at any
  age.

## Observability Evidence:

No-Observability-Change: the read records the same histograms, labels,
delta-union counter, span event, and WARN lines; resolution still covers the
reached scopes only, so the counters keep their meaning.
