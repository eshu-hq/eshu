# Readiness repository arm scope (#7088)

The supply-chain impact readiness snapshot was slow on the QA environment for a
repository-only anchor (`$11 = 'repository:r_…'`, `$20 = false`). Teammate
measurements on the QA environment (PG 18.3, 2026-10-02, read-only): 6 of 7
repository-anchor calls on the cold read replica hit the 30 s client timeout
(`readiness_snapshot` stage about 31 s), one stage took 52 s, and one call
returned HTTP 500 after 2.09 s, which is the 2 s replay-fence bound and
unrelated to query cost. The replica logged cancels for these statements (one
"conflict with recovery", one "user request"). A full-statement run on the
replica was cancelled at the teammate's own 20 s `statement_timeout`. A
40-scope slice of the legacy arm took 14.66 s cold and 0.50 s warm on the
replica. NOT_CHECKED: the 52 s stage, and which mechanism ends a given call.
This note records the measured plan cost, the change, and the proof.

## Query shape

Statement: `ListReadinessQuery`
(`go/internal/query/supply/chain/impact`), backend PostgreSQL 18.

- `package_manifest_active`, arm 1: dependency variables with
  `entity_metadata.config_kind = 'dependency'`.
- `package_manifest_active`, arm 2 (#7301): the legacy payload shape with a
  top-level `config_kind = 'dependency'`.
- `package_dependency_gap_active`: `entity_metadata.config_kind` is one of
  `vcs_dependency`, `path_dependency`, `url_dependency`,
  `editable_dependency`, `unsupported_dependency`.

All three now start from the repository's own scopes
(`ingestion_scopes.source_key = $11`, which covers repository and
`repository_ref` scopes). That is a PRECONDITION of the rewrite: old and new
agree only if every active git dependency-variable fact sits in a scope whose
`source_key` equals the fact's payload `repo_id`. Collector-written scopes hold
it (`buildScope` in `go/internal/collector/repo/git/source_processing.go`
stamps both from `repo.ID`; `TestBuildScopeRepositorySourceKeyMatchesMetadataRepoID`
pins it), and main's gap CTE already depended on it (#7007). But
`scopestore.SourceKey` falls back to the scope id when scope metadata has no
`source_key`, and `parserfixture.Emitter` builds scopes with no metadata, so
a scope written that way would drop out of both arms. Evidence we have
(teammate-reported, read-only, 2026-10-02): all 799 repository scopes on
the QA environment satisfy `scope_id = 'git-repository-scope:' || source_key` and the
scope payload `repo_id` equals `source_key`; a 50-scope sample of 117,010
active `content_entity` rows had 0 whose payload `repo_id` differs from the
scope's `source_key`. NOT_CHECKED: a fleet-wide fact-level count (an unbounded
scan). Each probes `fact_records` once per scope through a
`CROSS JOIN LATERAL (... OFFSET 0)`, so repository, scope and active
generation are all in the Index Cond. Arm 1 uses migration 121's index, whose
key is `((payload->>'repo_id'), scope_id, generation_id)`; before this change
the arm-1 plan bound only `repo_id` in its Index Cond, and the LATERAL probe
now binds all three. Arm 2
and the gap read use migration 159's new
`fact_records_content_entity_dependency_legacy_gap_repo_idx`, on
`((payload->>'repo_id'), scope_id, generation_id)` with a partial predicate
equal to those two readers' predicates joined by OR. The former
`$11 = '' OR` escape is removed. By code reading it was unreachable: every
consumer of the CTE requires a non-empty `$11` or `NOT $20`, and an empty `$11`
arrives with a target anchor that sets `$20`.

Rejected shapes, measured on a local scratch corpus modeled on the QA environment (803
scopes, not a QA count; the target with 16 superseded generations):

- `fact.scope_id = ANY(ARRAY(...)) AND fact.generation_id = ANY(ARRAY(...))`.
  The planner chose migration 003's `fact_records_active_package_dependency_entity_idx`,
  where `generation_id` is the fifth key column. That is a whole-index scan
  of every repository's dependency rows (3,750 buffers on the seed, which
  grows with the fleet).
- A plain join with the `source_key` pin and no LATERAL fence. The planner
  started from the repo-only Index Cond and heap-fetched every generation's
  rows: 9,362 fetched to keep 558 (30,263 buffers), the same shape as
  QA's 8,832 fetched to keep 552.
- Pinning with `scope_id = 'git-repository-scope:' || $11`. This is not
  equivalent: it drops every `repository_ref` scope
  (`git-repository-scope:<repo>@<ref>`).

## Measurements

Performance Evidence: The QA figures below were reported by a teammate who
ran read-only EXPLAIN (ANALYZE, BUFFERS) on the QA environment, PG 18.3, on 2026-10-02.
They were not re-measured for this note:

Scope denominators: the QA environment had 819 active scopes in total at the 2026-10-02
measurement, of which 799 are repository scopes (loop counts and the 43 below
use these). The ~803 and ~810 figures in older text came from earlier
measurements (the #7007 read, and the local scratch corpus); their exact dates
were not recorded, and they are not reconciled with 819/799.

- Base statement: ~9.4 s on the warm primary. On the cold read replica the
  repository-anchor calls hit the 30 s client timeout (see the top of this
  note). About 97% of its buffers went to arm 2, which probed
  `fact_records_collector_status_active_idx` once per active scope (819 loops)
  and filtered ~2.7k rows from each.
- Arm 1 fetched 8,832 rows to keep 552 active ones.
- The gap read scanned the anchored repository's whole active scope (up to
  241,726 rows; 43 of 799 scopes have at least 10k active content_entity rows).
- Scope-pinned shapes without the new index ran in 61-177 ms warm. Cold, in a
  sample of 3 repositories with 10k+ rows, they took 3.5-8.3 s (a sample, not
  a bound), and the remaining cost was arm 2 and the gap read scanning the one
  active scope.
- With the `$11 = '' OR` escape kept, the statement timed out at 20 s under
  `force_generic_plan`. Without it, the three dependency-variable arms ran in
  79-191 ms. That figure covers those arms only, not the whole statement.

Local figures were measured by this change on 2026-10-02, against a disposable
`postgres:18-alpine` with the real bootstrap applied. The corpus is the one in
`TestSupplyChainImpactReadinessRepoArmScopeLive`: 650 noise repository scopes
with 300 content_entity rows each plus one row of each dependency shape. The
target repository has 16 superseded generations of 550 dependency rows and an
active generation with 20,000 non-matching rows plus every matching shape and
tombstoned copies. It also has a `repository_ref` scope with its own rows.
There is a ref-only repository and an unknown repository.

| Target repository, one statement | Before (origin/main) | After |
| --- | --- | --- |
| Custom plan, dependency-variable reads | 211,558 shared buffers; arm 2 654 loops on `fact_records_collector_status_active_idx`; arm 1 Index Cond repo_id only | 588 shared buffers; 3 scans, loops ≤ 2, Index Cond (repo_id, scope_id, generation_id) on the 121 and 159 indexes |
| Custom plan, whole statement | 238,642 shared buffers | 6,753 shared buffers |
| Generic plan (`force_generic_plan`), dependency-variable reads | 204,290 shared buffers; arm 1 a full scan of `fact_records_active_package_dependency_entity_idx` (empty Index Cond); arm 2 654 loops | 588 shared buffers; same bounded shape as custom |

Re-run at the commit titled "docs(postgres): state measured samples and the
source_key precondition for the readiness repo arms (#7088)" (the last commit
that touched the live proof, the query SQL or the migration; later commits
change only the evidence note, the package README and the static-guard test
helper, so the live proof and the SQL are unchanged since) with `go test ./internal/query/supply/chain/impact -run 'RepoArm|PackageManifestRepoScope|ScanTier' -count=1 -v`
against a disposable PG 18.6 container: all tests PASS, exit 0. The custom
plan reported dependency-scan buffers 588 and statement shared buffers 6,753;
the generic plan reported 588 and 1,071,445. These equal the table values.

The whole statement under the forced generic plan stays near 1.07M buffers
(1.30M before). That cost is in other CTEs (`package_consumption_correlation_active`
and the advisory family), which filter on `fact_kind = ANY($n)` and, on this
corpus, probe every active scope under a generic plan. #7088 does not change
them.

The 79-191 ms QA figure above is an arm-level measurement of the three
dependency-variable reads only, so it says nothing about those other CTEs. The
whole statement was measured separately on the QA environment (next section).

### Whole statement on the QA environment, new query text against the old schema

Measured by a teammate, read-only, 2026-10-02 18:25 to 18:28 UTC, on the read
replica (PostgreSQL 18.3, in recovery, no peer activity at start or before the
last runs; 819 active scopes, 799 of them repository scopes; `fact_records`
about 137.9 M rows, 186 GB; `shared_buffers` 4 GiB). The query text is the
shipped `ListReadinessQuery` of this branch with the 20 arguments bound as
production binds them for a repository anchor. The schema is QA's, which
does not have migration 159, so the manifest and gap reads scan the anchored
scope as they did before the index; the figures are therefore without the new
index. Custom plan = `PREPARE` plus `SET LOCAL plan_cache_mode =
force_custom_plan` plus `EXPLAIN (ANALYZE, BUFFERS, TIMING OFF) EXECUTE`;
auto = `plan_cache_mode = auto`, `PREPARE` then 8 plain `EXECUTE`s with
`pg_prepared_statements` read afterwards. Active `content_entity` rows come
from a sampled count over 45 scopes.

| Repository size class | Custom plan | Forced generic plan | auto, 8 executions |
| --- | --- | --- | --- |
| small, 1,042 rows | 143.6 ms, 22,096 buffers | 109.1 ms, 34,496 buffers | 103-173 ms, 0 generic plans |
| medium, 9,127 rows | 166.9 ms, 26,090 buffers | 112.5 ms, 38,490 buffers | 114-186 ms, 0 generic plans |
| large, 21,824 rows | 226.2 ms warm, 147,139 buffers; first run 1,550 ms (cache state not controlled) | 262.7 ms, 159,539 buffers | 255-328 ms, 0 generic plans |
| 59,033 rows (extra) | 245.1 ms warm, 60,440 buffers; first run 375 ms (cache state not controlled) | 241.4 ms, 72,877 buffers | 225-300 ms, 0 generic plans |

These figures are not comparable with the 30 s timeouts of the HTTP calls
earlier in this note: those are end-to-end API calls on the replica, while
these are statement-level `EXPLAIN ... EXECUTE` timings with a warm cache and
different start and end events. The closest statement-level before figures are
the old text's 9.4 s on the warm primary and its run on the replica that the
teammate's own 20 s `statement_timeout` cancelled (cache state not recorded).
The figures here are also without migration 159.

Reading it:

- A forced generic plan cost 8% to 56% more buffers than the custom plan
  (+56% small, +48% medium, +8% large, +21% for the 59k-row repository) and
  ran in the same wall-time class. The extra is about 12.4k buffers on every
  repository, so it follows the 819 active scopes and not the repository's
  size: the advisory and exploitability arms lose a cheap index path because
  `$13` is not constant-folded, and the container-image-identity CTE (5,850
  buffers) is skipped by the custom plan.
- `plan_cache_mode = auto` never adopted the generic plan: `generic_plans` was
  0 after 8 executions on all four repositories. The generic plan's estimated
  cost (3,732.90) was above the custom average (2,671.65). An anchor where that
  estimate flips was not tested. pgx's binary parameter typing was not
  exercised and could shift that cost comparison.
- The ~1.07 M-buffer figure on the local corpus did not reproduce. On the QA environment
  the per-scope probes return 0 rows at about 5 buffers per scope; the local
  figure is about 1,650 buffers per scope. The cause of the local figure is not
  established. An earlier guess, that the local corpus holds rows of those fact
  kinds in every scope, is contradicted by the seed, which inserts only
  `content_entity` facts; the local per-scope cost may come from the tiny
  corpus's statistics or plan choice, which was not investigated.
- The largest cost on the large repository was the anchored-scope scan
  (`unsupported_target_rows` 67,674 buffers, `package_manifest_active` 63,248),
  which migration 159 targets, not the plan cache.

Limits: one run per cell for custom and generic, 8 for auto; warm replica
cache, so these are not p50 or p95 latencies; the first run on the two larger
repositories is shown separately and its cache state was not controlled; psql text-literal parameters,
not pgx binary typing; CVE, package and digest anchors, a deployment whose
per-scope probes are not index-bounded, the primary's plans and behaviour under
concurrent load were not measured. This is not a measurement of the new index
on the QA environment, and it does not replace the after-number the owner's deploy makes
possible.

The new index on the local corpus is 98,304 bytes, covering 1,621 of 281,816
`fact_records` rows. The seed deliberately puts one legacy and one gap row in
each of the 650 noise scopes. Migration 121's index is 245,760 bytes over
10,162 rows. Before deployment, the teammate measured 0 arm-2 matches and
17 gap matches on the QA environment and expected a small index. The deployed standby
size, measured later, is six 8 KiB pages as recorded below.

Insert tax and build time, LOCAL measurement (2026-10-02, disposable
`postgres:18-alpine` container (18.6), real bootstrap schema with all 177 migrations, so
every other `fact_records` index is present; one 200,000-row server-side
`INSERT ... SELECT` into `fact_records` per run, 800 scopes, git
`content_entity` rows with a Function/Variable mix, 16 of them matching the
index predicate; `DELETE`, `VACUUM FULL`, `VACUUM ANALYZE` and `CHECKPOINT`
before each run; runs alternated without/with, 3 each):

| Variant | Runs (s) | Median (s) | Spread (s) |
| --- | --- | --- | --- |
| Without the index | 5.478, 5.635, 5.685 | 5.635 | 0.207 |
| With the index | 5.757, 5.683, 5.660 | 5.683 | 0.097 |

The median difference is +0.048 s (+0.9%), inside the spread of the
no-index runs, so this measurement cannot separate the tax from noise.
`CREATE INDEX CONCURRENTLY` on the populated 200,000-row table took 0.058,
0.058 and 0.059 s and produced a 16,384-byte index (16 predicate rows); the
heap was 86,237,184 bytes. These are LOCAL numbers from a single-session
insert, not a QA build time. At the 2026-10-02 pre-deploy observation,
the QA heap was reported at about 183 GB and 138M rows. The later
standby size and deployed build duration are recorded below; build-attributable
replica conflicts and the real QA insert tax remain NOT_CHECKED. The
local insert-run raw log is not committed. Every git
`content_entity` insert or update also evaluates the partial predicate
(a few JSONB extractions); only matching rows pay index maintenance.

### Deployed after-index read-only follow-up (2026-10-03)

The sanitized plan, endpoint sample, and material-data count inputs are in
[the deployed run record](7088-opsqa-migration159-run-20261003.md). It binds
the measurements to the deployed image and query source without publishing
repository identifiers or credentials.

Migration 159 is deployed on the QA environment. The schema Job logged 1,221,522 ms
(20m21.522s) for its concurrent index build, completing at 03:08:09 UTC.
The streaming reader's cumulative `confl_snapshot` counter was 7 when
checked afterwards; without a before counter and a matching stats-reset
boundary, none of those conflicts can be attributed to the build.
The deployed index occupies 49,152 bytes (six 8 KiB pages) on the standby
versus 208,274,554,880 bytes (about 194 GiB) for the `fact_records` heap.
The standby reported 1,049 scans of that index at observation time; this
counter has no before-build comparator.

A temporary, uncommitted Go diagnostic passed the shipped
`ListReadinessQuery` and its production-shaped 20 repository arguments to
`EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)` in one read-only,
repeatable-read transaction on the QA PostgreSQL standby. It checked
`pg_is_in_recovery()` before querying. Four deterministic repository
anchors had approximately 1,113, 8,452, 19,804, and 36,678 active
`content_entity` rows in earlier bounded counts. Each of the three
dependency-variable probes used the repository-leading partial index
from migrations 121 or 159 exactly once. The two migration-159 probes
each hit two shared buffers and read none in every plan.

| Active-row class | Full statement execution | Statement shared hit/read blocks | Dependency-variable index probes |
| --- | ---: | ---: | --- |
| 1,113 | 63.655 ms | 20,359 / 0 | 3, each 1 loop |
| 8,452 | 56.883 ms | 20,217 / 6 | 3, each 1 loop |
| 19,804 | 94.097 ms | 22,855 / 21 | 3, each 1 loop |
| 36,678 | 57.460 ms | 20,947 / 0 | 3, each 1 loop |

The matching deployed API route (`GET /api/v0/supply-chain/impact/findings`,
`profile=precise`, `limit=1`, repository anchor,
`Accept: application/eshu.envelope+json`) returned HTTP 200 on 32/32
serial calls, eight per anchor. Its truth envelope reported `exact` and
`fresh`; the readiness snapshot reported `fresh` and
`evidence_incomplete` on every call. The canonical readiness payload
digest was stable within each anchor. These reported labels and stable
digests do not independently prove that the verdict or freshness is
correct against source facts.

| Active-row class | API median | Eight-sample nearest-rank p95 |
| --- | ---: | ---: |
| 1,113 | 0.204979 s | 0.226672 s |
| 8,452 | 0.202595 s | 0.544880 s |
| 19,804 | 0.224139 s | 0.265203 s |
| 36,678 | 0.210831 s | 0.214650 s |

One additional material-data truth probe selected an active repository
through the five dependency-gap `config_kind` values using the standby's
active-generation rows, without printing its identifier. An independent
read-only count found five active gap facts for that repository. The
deployed endpoint returned five `dependency_source` unsupported targets,
`readiness_state=unsupported`, and an `exact`, `fresh` truth envelope in
0.489043 s (HTTP 200). This is a direct count match for one populated
repository gap case, not proof of every readiness family or every anchor.

These are after-index absolute latencies, not a matched before/after
speedup: the earlier table used different repository anchors and an old
schema, and the live reader continued replaying writes during this run.
Cache state was warm or unknown, so the cold-cache p95 remains NOT_CHECKED.
The requested approximately 59k-row class was not found by a bounded
45-repository sample; that sample's read-only index-count plan took
4,321.415 ms and read 19,227 blocks, so the search was not widened on
the live replica. Populated non-repository anchors, full independent result
truth beyond the five-fact gap case, an attributable build-time
replica-conflict delta, and deployed insert tax also remain NOT_CHECKED.
Do not close #7530 or #7088 on these numbers alone.

For #7530's insert-cost alternative, a live before/after writer test would
require dropping and rebuilding the production index that took 20m21.522s
to build, while ingest and standby replay continue. That change is not
justified solely to measure a possible tax. The controlled interleaved
200,000-row local insert run above found a +0.9% median difference inside
its own spread, and the deployed index footprint is 49,152 bytes. Those
facts support avoiding a disruptive live A/B; they do not establish zero
predicate-evaluation or index-maintenance cost on the QA writer.

No-Observability-Change: No new runtime signal was added. The readiness read
is already timed by the existing `readiness_snapshot` stage timing, and
operators can see the effect there. The change only alters the plan of an
existing statement.

## Correctness proof

`TestSupplyChainImpactReadinessRepoArmScopeLive` makes these checks:

- It compares every result row of the rewritten statement with the previous
  statement. The previous statement is rebuilt by splicing the old CTE texts
  into the shipped constant, guarded by
  `TestPreviousRepoArmReadinessQuerySplicesOnlyTheTwoCTEs`. The comparison
  covers the target repository (repository plus `repository_ref` scope), a
  repository with only a `repository_ref` scope, and an unknown repository.
- It pins absolute counts: 562, 6 and 0 `package.consumption` facts, and 10,
  5 and 0 dependency-source gap targets. The counts include only active
  generations and exclude tombstones.
- It repeats the comparison for 7 EXECUTEs of a SQL-level prepared statement
  under `plan_cache_mode = force_generic_plan`.

The RED run (the regression-test commit that precedes the fix in this PR) used the origin/main SQL and schema. It failed
only on plan shape: 654 loops, the missing index, and generation not in the
Index Cond. Its row comparison and counts passed.

The live test is scheduled-class and no CI workflow runs it. The CI-run guards
in `readiness_repo_arm_static_guards_test.go` pin the three `OFFSET 0` fences,
the `scope.source_key = $11` anchors, the absent `$11 = '' OR` escape, and the
equality of the gap `IN` list and the legacy `config_kind` predicate with the
embedded migration's index predicate. They also pin (counts taken on text with
every `--` comment removed, whole-line or trailing, so a needle left in a
comment cannot satisfy them; `TestStripSQLLineCommentsDropsEverySQLComment`
pins the helper and `TestReadinessRepoArmGuardedSQLHasNoDoubleDashInsideLiterals`
pins that the cut cannot corrupt a literal; `TestReadinessRepoArmGuardedSQLHasNoBlockComments`
rejects `/* */` comments, which the helper does not strip; the `OFFSET 0` fence,
`scope.source_key` anchor and LATERAL-probe counts use the same stripped text)
three `dependency.generation_id =
scope.active_generation_id` binds, three `dependency.is_tombstone = FALSE`
filters and three `dependency.payload->>'repo_id' = $11` binds across the two
manifest arms and the gap CTE, and arm 1's `NULLIF(...
config_kind, '') IS NULL` exclusivity and arm 2's `config_kind = 'dependency'`
each exactly once. An independent reviewer showed that the earlier helper
removed only whole-line comments, so a needle in a trailing comment kept a
guard green after its predicate was deleted; the helper was fixed and that
exact violation now fails the tombstone guard (2 occurrences, want 3). `TestReadinessEmptyRepositoryAlwaysCarriesATargetAnchor`
calls the production `hasFactAnchor`, `needsTargetResolution` and
`readinessTargetArguments` for each of the CVE-only, package-only,
subject-digest-only and image-ref-only anchors with an empty repository id and
requires `$20 = true`; a repository-only anchor requires `$20 = false`. That
implication is what made the removed `$11 = ''` escape unreachable. Each guard
was shown RED by temporarily mutating the production constant or the migration
SQL, and GREEN on the clean tree.

Two independent reviewers then showed that conditions inside the probes were
still unbound: the `dependency.scope_id` bind, `fact_kind`, `source_system`,
`entity_type` and the outer generation join. Dropping `fact_kind`,
`source_system` or `entity_type` from a probe breaks predicate implication for
migrations 121 and 159, so the index cannot be used, and none of that changes
the text shapes the needles look for. `TestReadinessRepoArmExecutableTextIsPinned`
closes the class by comparing the executable SQL of `package_manifest_active`
and `package_dependency_gap_active` (comments stripped, whitespace collapsed)
with `testdata/readiness_repo_arm_probes.golden`. Dropping any of those
conditions from any probe fails it (seeded: scope bind in arm 2 and the gap
probe, `fact_kind` in arm 1 and the gap probe, `source_system` in arm 2,
`entity_type` in the gap probe, the outer generation join in arm 1); adding a
comment or blank line does not. It is a change detector: a deliberate edit to
either statement must regenerate the golden with `-update-repo-arm-golden` and
carry the plan proof that justifies it. It does not check that the plan is
good, only that the text that produced the measured plan is the text shipped;
the scheduled live proof remains the only check of the plan itself.

The scan-tier live proofs (`readiness_scan_tier_explain_live_test.go`) bound 16
arguments to the 20-parameter statement and failed on main with `expected 20
arguments, got 16`. They now bind all 20 through `readinessArgsForQuery`, which
takes `$17`-`$20` from `readinessTargetArguments`, and pass against a
disposable PG 18.

Commands:

```bash
ESHU_PACKAGE_MANIFEST_REPO_SCOPE_EXPLAIN_PROOF_DSN='postgres://postgres:…@127.0.0.1:<port>/postgres?sslmode=disable' \
ESHU_PACKAGE_MANIFEST_REPO_SCOPE_EXPLAIN_PROOF_DISPOSABLE=1 \
ESHU_SCAN_TIER_READINESS_EXPLAIN_PROOF_DSN='postgres://postgres:…@127.0.0.1:<port>/postgres?sslmode=disable' \
ESHU_SCAN_TIER_READINESS_EXPLAIN_PROOF_DISPOSABLE=1 \
  go test ./internal/query/supply/chain/impact -run 'RepoArm|PackageManifestRepoScope|ScanTier' -count=1 -v
```
