# Readiness repository arm scope (#7088)

The supply-chain impact readiness snapshot was slow on ops-qa for a
repository-only anchor (`$11 = 'repository:r_…'`, `$20 = false`). Teammate
measurements on ops-qa (PG 18.3, 2026-10-02, read-only): 6 of 7
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
ops-qa satisfy `scope_id = 'git-repository-scope:' || source_key` and the
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

Rejected shapes, measured on a local scratch corpus modeled on ops-qa (803
scopes, not an ops-qa count; the target with 16 superseded generations):

- `fact.scope_id = ANY(ARRAY(...)) AND fact.generation_id = ANY(ARRAY(...))`.
  The planner chose migration 003's `fact_records_active_package_dependency_entity_idx`,
  where `generation_id` is the fifth key column. That is a whole-index scan
  of every repository's dependency rows (3,750 buffers on the seed, which
  grows with the fleet).
- A plain join with the `source_key` pin and no LATERAL fence. The planner
  started from the repo-only Index Cond and heap-fetched every generation's
  rows: 9,362 fetched to keep 558 (30,263 buffers), the same shape as
  ops-qa's 8,832 fetched to keep 552.
- Pinning with `scope_id = 'git-repository-scope:' || $11`. This is not
  equivalent: it drops every `repository_ref` scope
  (`git-repository-scope:<repo>@<ref>`).

## Measurements

Performance Evidence: The ops-qa figures below were reported by a teammate who
ran read-only EXPLAIN (ANALYZE, BUFFERS) on ops-qa, PG 18.3, on 2026-10-02.
They were not re-measured for this note:

Scope denominators: ops-qa had 819 active scopes in total at the 2026-10-02
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

Re-run at code/test commit `ddc43b5d7` (the last commit that changed Go or
SQL; later commits are docs only) with `go test ./internal/query/supply/chain/impact -run 'RepoArm|PackageManifestRepoScope|ScanTier' -count=1 -v`
against a disposable PG 18.6 container: all tests PASS, exit 0. The custom
plan reported dependency-scan buffers 588 and statement shared buffers 6,753;
the generic plan reported 588 and 1,071,445. These equal the table values.

The whole statement under the forced generic plan stays near 1.07M buffers
(1.30M before). That cost is in other CTEs (`package_consumption_correlation_active`
and the advisory family), which filter on `fact_kind = ANY($n)` and, on this
corpus, probe every active scope under a generic plan. #7088 does not change
them.

NOT_CHECKED: the whole-statement cost under a generic plan on ops-qa. The
79-191 ms ops-qa figure above is an arm-level measurement of the three
dependency-variable reads only, so it says nothing about those other CTEs. This
change does not claim the replica timeout is gone under a generic plan.

The new index on the local corpus is 98,304 bytes, covering 1,621 of 281,816
`fact_records` rows. The seed deliberately puts one legacy and one gap row in
each of the 650 noise scopes. Migration 121's index is 245,760 bytes over
10,162 rows. On ops-qa the arm-2 predicate matches 0 rows and the gap
predicate matches 17 (teammate measurement), so the index is expected, not
measured, to be a page or two.

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
insert, not an ops-qa build time: ops-qa has a 183 GB heap and about 138M
rows, and the build time, the build's effect on the replica, and the real
insert tax there are NOT_CHECKED until the owner deploys the migration. The
raw log is not committed. Every git `content_entity` insert or update also
evaluates the partial predicate (a few JSONB extractions); only matching rows
pay index maintenance.

NOT_CHECKED: the cold-replica after-number with migration 159 applied. It
stays unknown until the owner deploys the migration to ops-qa and reruns
EXPLAIN (ANALYZE, BUFFERS) for a 10k+ row repository anchor.

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
embedded migration's index predicate. They also pin (code-shaped needles that
a SQL comment cannot satisfy) three `dependency.generation_id =
scope.active_generation_id` binds and three `dependency.is_tombstone = FALSE`
filters across the two manifest arms and the gap CTE, and arm 1's `NULLIF(...
config_kind, '') IS NULL` exclusivity and arm 2's `config_kind = 'dependency'`
each exactly once. `TestReadinessEmptyRepositoryAlwaysCarriesATargetAnchor`
calls the production `hasFactAnchor`, `needsTargetResolution` and
`readinessTargetArguments` for each of the CVE-only, package-only,
subject-digest-only and image-ref-only anchors with an empty repository id and
requires `$20 = true`; a repository-only anchor requires `$20 = false`. That
implication is what made the removed `$11 = ''` escape unreachable. Each guard
was shown RED by temporarily mutating the production constant or the migration
SQL, and GREEN on the clean tree.

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
