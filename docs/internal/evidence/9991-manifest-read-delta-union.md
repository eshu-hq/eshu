# #9991 Manifest Dependency Read: Delta-Active Scopes Keep Their Base Pins

`FactStore.ListActivePackageManifestDependencyFacts` joined
`fact.generation_id = scope.active_generation_id`. A git delta generation
stores only its changed files' rows, so a delta-active repository returned only
the delta's pins and lost every unchanged pin of its base full generation. The
#7797 overlay ran only at projector load time. Reproduced on PostgreSQL 18.6:
a full with `alpha` and `bravo`, then an active delta that re-emits `alpha`,
loaded `[delta-alpha]`.

## Change

`go/internal/storage/postgres/active_package_manifest_dependency_delta.go`
(since #10003 the reader-agnostic `delta_overlay.go`, `delta_overlay_query.go`,
and `delta_union_base.go`; see
[10003-delta-overlay-helper.md](10003-delta-overlay-helper.md)):

1. The active query pages unchanged.
2. One global lookup (`manifestDeltaTargetsQuery`, now `deltaOverlayTargetsQuery`) names every delta-active
   scope. It must be global: a delta that touched no manifest leaves its
   scope with base pins and no active row. With no delta-active scope that
   has a baseline, the read stops here: active pages plus this one statement.
3. Otherwise a base-candidate query, derived from the active text by
   `deltaBasePackageManifestDependencyQuery` (now `deriveDeltaOverlayBaseQuery` at `$6`; two fragment swaps, fail-closed
   shape check), pages through the same partial indexes, keyset cursor, and
   limit, restricted to those scopes (`fact.scope_id = ANY($6)`). It selects
   activated full generations at the delta's union-base commit,
   `COALESCE(delta_union_base_commit_sha, delta_baseline_commit_sha)`, a
   strict superset of `deltaUnionBaseQuery`. #9611 rewrites the active text,
   and the base text follows because it is derived.
4. For each delta-active scope the rows reached, `resolveDeltaBaseAtCommit`
   picks the base the projector overlays, including the retention prune
   probe and the refusal.
5. One batched read (`manifestDeltaOverlayInputsQuery`, now `deltaOverlayInputsQuery`) loads each delta's
   repository fact and its stable-key collisions, tombstones included. Then
   `facts.OverlayDeltaBase` keeps the base rows the delta did not override,
   tombstone, or touch.
6. All statements share one read-only repeatable-read snapshot. Inside the
   impact snapshot the transaction nests as a no-op.

A refused base, a legacy delta with no baseline, or a delta with no
`delta_generation` repository fact keeps today's delta-only rows. A lookup
error fails the read.

Rejected: one combined statement, active OR base generation per row. It read
the same 21,205 rows at a 6.16 s median against 4.47 s for the two paged
statements on the same data. A SQL copy of the base rule and the overlay was
also rejected: it would be a second definition of #7797.

## Performance Evidence:

Source `95fa47b4be2f` (branch `lane3/9991-delta-manifest-read`). Disposable
PostgreSQL 18.6 container, image
`sha256:74935e72241653ca55e0414067e6d8763aceb8a810eb51b452253ec3dcfc4336`
(`postgres:18`, default settings), on a shared developer host. The schema is
`ApplyBootstrap`. The corpus has 2,001 git scopes. 2,000 of them carry 25 npm
pins in each of a superseded and a current full (100,000 rows), and 200 of
those are delta-active with a 5-row delta. The `big` scope has a 50,000-pin
base over 500 manifests and an active 5,000-row delta over 50 of them. That is
156,201 fact_records in total, with 400 names in `$2`. The harness was a
throwaway, uncommitted test calling the production code over `SQLDB`. "Old"
pages the shipped active query only, the statements the read sent before
#9991. "New" is the whole `ListActivePackageManifestDependencyFacts`. Each
figure is the median of seven runs. The statement-count section below uses
the committed harness. A rerun after the exec-mode fix, on
the same corpus but a loaded host (load average 18.6 on 12 cores), gave old
2.012 s and new 5.741 s for the npm delta-active shape. The ratio held
(2.85x against 2.99x) with more noise, so the table keeps the first run.

| shape | old rows | old median | new rows | new median |
| --- | --- | --- | --- | --- |
| npm, 201 delta-active scopes | 11,172 | 1.513 s | 20,000 | 4.518 s |
| npm, same corpus with every full active (no delta) | 20,000 | 4.116 s | 20,000 | 4.322 s |
| maven, no matching rows | 0 | 9.2 ms | 0 | 22.9 ms |

The first row is not a like-for-like comparison: the old read returned the
wrong, short row set. The 8,828 extra rows match an independent SQL count of
the base pins the delta did not override or touch (11,172 + 8,828 = 20,000).
The comparable figures:

- The same 20,000 pins read through a delta-active corpus cost 4.52 s,
  against 4.12 s read from active fulls (+9.8 %).
- This table predates the no-delta gate. Without the gate, a corpus with no
  delta-active scope paid one extra base page, the target lookup, and
  BEGIN/COMMIT: +0.21 s (+5 %) here, and +14 ms (mostly planning) for an
  empty read. With the gate, that case adds only the target lookup; see
  "Statement counts and the no-delta gate" below.

`EXPLAIN (ANALYZE, BUFFERS)`, page 1, delta-active shape:

| statement | time | buffers | plan |
| --- | --- | --- | --- |
| active page (npm) | 212.8 ms | 284,936 | `fact_records_active_package_dependency_entity_idx`, nested loop, top-N sort |
| base page (npm) | 278.0 ms | 401,513 | same index, same shape, plus the `active_delta` probe by `scope_generations_active_scope_idx` |
| active page (maven) | 0.13 ms | 9 | same index, sbt branch |
| base page (maven) | 0.16 ms | 9 | same index, sbt branch |
| delta targets (global lookup) | 1.89 ms | 93 | `scope_generations_active_pending_activity_idx` |
| overlay inputs, hand-built over all 201 delta-active pairs and 15,000 keys | 4.16 ms | 2,119 | `(scope_id, generation_id)` index prefix |
| base lookup, one scope | 0.05 ms | 8 | `scope_generations_scope_idx` (unchanged #7797 query) |
| prune probe, one scope | 0.008 ms | 0 | `generation_retention_events_generation_idx` |

The overlay inputs read filters each delta's rows by
`stable_fact_key = ANY($3)`, a residual Filter: this is the #9586 trap. Under
`PREPARE` with `plan_cache_mode` forced each way (psql, same corpus, same
arrays), the custom plan took 14.9 ms and the generic plan took 1,077.8 ms,
with the same plan and the same 201 index searches. The overlay inputs read
therefore runs with `manifestDependencyQueryExecMode`
(`QueryExecModeCacheDescribe`), the same mode as the manifest pages. The
target lookup binds no array.
`TestListActivePackageManifestDependencyFactsDeltaAuxReadsUseCustomPlans`
pins the mode; removing it fails the test.

The overlay inputs row is a worst-case statement built by hand over every
delta-active scope. The read itself sends that statement only for the scopes
it reached, which was 4 here (next section).

No statement runs a sequential scan. The base page has the active page's
shape. Each page costs one index-prefix scan, the same as the active read's
existing paging (#8961). The next measured bottleneck is that per-page prefix
scan, which predates this change.

## Statement counts and the no-delta gate

A perf run against an earlier head (565k dependency rows, 400 scopes, 40
delta-active) found the base scan ran on every read: about +19 % over a
16-page read and roughly double a short read, even when no matched scope was
delta-active. Gating on the matched scopes alone would drop base-only scopes
(`DeltaDeletesManifestPath`, `DeltaMavenFamily`), so the gate is the global
target lookup above. Red first:
`TestListActivePackageManifestDependencyFactsNoDeltaSkipsBaseRead` asserts
two statements (active page, target lookup) and no base scan for a no-delta
corpus; it failed on the ungated head.

The committed harness
`go/internal/storage/postgres/active_package_manifest_dependency_delta_bench_test.go`
(build tag `perf9991_manifest_delta`, command in its header) seeds the same
corpus, wraps the database and the read's transaction, and counts every
statement by kind. It counts BEGIN and COMMIT apart; the statement totals
below exclude them, and every new read adds exactly one of each. Medians of
five runs at head `aef18026e7` plus the harness, on a host at load average
3.6 to 10 on 12 cores:

| state | read | old statements | old median | new statements | new median |
| --- | --- | --- | --- | --- | --- |
| delta-active | npm | 23 | 1.582 s | 54 | 3.573 s |
| delta-active | maven, no rows | 1 | 10.1 ms | 3 | 53.5 ms |
| no delta-active scope | npm | 41 | 4.346 s | 42 | 4.188 s |
| no delta-active scope | maven, no rows | 1 | 9.8 ms | 2 | 10.6 ms |

The delta-active npm read reached 4 delta-active scopes: `big` and three
small ones. Only those 4 have rows matching the 400 names; the other 197
delta-active scopes match none. Its 54 statements break down as:

- 23 active pages, 1 target lookup, and 21 base pages;
- per reached scope, 1 base lookup and 1 prune probe (every base is
  superseded once its delta activates): 4 + 4;
- 1 overlay inputs read.

So base resolution costs 2 statements per reached scope, and the total grows
linearly with reached scopes. The Maven reads reach none: their 3 statements
are the active page, the target lookup, and one empty base page.

In the no-delta state the only added statement is the target lookup (0.46 ms
in `EXPLAIN ANALYZE`); the npm medians agree within noise. With 201
delta-active scopes the lookup takes 1.89 ms (93 buffers). The base page
restricted to them still scans the package-manager prefix (157,799 index
buffers, 265.6 ms against 278.0 ms unrestricted), because
`fact.scope_id = ANY($6)` is a residual filter. The restriction shrinks only
the join work, 10,066 rows into the joins instead of 31,205. So while any
scope is delta-active, each base page costs about one active page. The #9611
per-name rewrite of the shared template is what shrinks both.

An earlier count on a loaded host (load average 12 to 20) gave the same
statement counts with noisier times: npm delta-active 2.428 s old against
5.004 s new, no-delta 5.651 s against 6.213 s.

## Known limits

- Base resolution sent one base lookup per reached delta-active scope, plus
  a prune probe for a superseded base, so round trips grew linearly with the
  reached scopes. #10003 batched both statements: base resolution is now at
  most two statements for all reached scopes (see
  [10003-delta-overlay-helper.md](10003-delta-overlay-helper.md)).
- The delta-target lookup is unbounded: it returns every delta-active git
  scope in the deployment on every read, whatever the read's names. It
  touches no fact rows (1.89 ms, 93 buffers at 201 delta-active scopes), and
  only the scopes the read's rows reach are resolved, but its result grows
  with the delta-active share of the fleet. The lane 1 QA readback of that
  share sizes it.
- The reducer now logs the existing projector `projector delta union fact
  load` INFO line when its `ListFacts` loads a delta generation, because the
  reducer FactStore carries the process logger (review P3 G2). That is one
  INFO line per delta load, the same line the projector already logs.
- Two activated fulls at one union-base commit both pass the base-candidate
  prefilter, so the base pages scan twice that scope's rows before Go keeps
  the resolved base's rows.
- The base pages scan the package-manager index prefix whenever any scope is
  delta-active (#9611 owns the rewrite).

## Observability Evidence:

- Counter `eshu_dp_package_manifest_dependency_read_delta_union_total` by
  `read` and the closed `outcome` set: `union_base_found`,
  `union_base_unmatched`, `union_base_missing`, `union_scope_mismatch`,
  `union_no_baseline`, `union_resolve_failed`. No scope, generation, or
  commit labels.
- One `package_manifest_dependency_read.delta_union` span event per read that
  reached a delta-active scope, carrying `read` and the decision count of each
  outcome. One event per read stays under the SDK's per-span event cap on a
  broad read.
- One WARN log for each `union_base_missing` and `union_scope_mismatch`
  decision, with the scope, generation, both commits, and `chained`. The
  reducer FactStore now carries the process logger (`newReducerFactStore`,
  `TestNewReducerFactStoreCarriesInstrumentsAndLogger`); before, the WARN
  never fired in production. The same logger turns on the existing #7797
  `projector delta union fact load` INFO line when a reducer `ListFacts`
  loads a delta generation; its counter already recorded there.
- The supply-chain impact loader's FactStore now carries the instruments and
  logger on the disengaged and the snapshot path
  (`NewAuthorizedSupplyChainImpactEvidenceLoaderWithTelemetry`). The impact
  read therefore records the delta-union counter and WARN, and the #9586
  `read_seconds` and `read_pages` histograms it never recorded before. The
  same instruments also feed the other FactStore metrics on that path, such
  as the supply-chain impact active-evidence metrics.
  `TestAuthorizedImpactLoaderCountsManifestDeltaUnionLive` (disengaged, no
  role policy) and `TestAuthorizedImpactLoaderCountsManifestDeltaUnionEngagedLive`
  (role policy installed, snapshot clone) each count a union decision and
  capture the base-missing WARN through the loader. Reverting the snapshot
  line to a bare `NewFactStore` fails the engaged test only.
- `union_resolve_failed` counts once per read whose delta-union step failed:
  the target lookup, a base page, a base resolution, or the overlay inputs
  read.
- The existing read histograms keep their meaning. `read_pages` counts active
  pages only, so the base read's cost shows as wall time while pages hold.

## Proof

- `go test ./internal/storage/postgres -run 'TestListActivePackageManifestDependencyFacts(Delta|Chained|FullActive)' -count=1`
  against the disposable DSN runs nine live cases. All but the full-active
  guard failed before the fix and pass after it.
  `TestAuthorizedImpactLoaderCountsManifestDeltaUnionLive` runs the read
  through the impact loader.
- Mutations: dropping tombstones from the overlay inputs fails the tombstone
  case. Resolving at the baseline alone instead of the union base fails the
  chained case.

## Readers with the same active-generation-only join

These join `scope.active_generation_id = fact.generation_id` over git-collected
facts, so a delta-active repository shows them delta-only rows. They are not
fixed here; #10003 tracks them.

- `facts_active_code_call_symbols.go:47` (`file`)
- `facts_active_jvm_reachability.go:43` (git `file`)
- `facts_active_crossplane_xrd.go:46` (git `content_entity`)
- `owned_package_targets.go:173` (git `content_entity`)
- `facts_active_cicd_workflow_image.go:42` (git `ci.workflow_image_evidence`)
- `eshu_search_document.go:186`, `eshu_search_vector_pending.go:42`,
  `code_drifted_findings.go:32`, `terraform_config_state_drift_findings.go:273`
  (the fact kind is a parameter; affected when it names a git kind)
- `facts_active_package_ownership.go:36`,
  `facts_active_security_alert_reconciliation.go:36`,
  `facts_active_sbom_attestation_attachment.go:38` (fact-kind lists; affected
  for any git kind they include)

`facts_active_repository.go:54` reads the `repository` fact, which every delta
carries. The other active readers join non-git sources (cloud, OCI, CI runs,
incidents) or `reducer_*` facts. Git delta generations do not apply to them.
