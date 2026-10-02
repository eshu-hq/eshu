# Readiness repository arm scope (#7088)

The supply-chain impact readiness snapshot timed out on ops-qa for a
repository-only anchor (`$11 = 'repository:r_…'`, `$20 = false`): about 31 s,
over the read replica's 30 s statement limit. This note records the measured
cause, the change, and the proof.

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
`repository_ref` scopes). Each probes `fact_records` once per scope through a
`CROSS JOIN LATERAL (... OFFSET 0)`, so repository, scope and active
generation are all in the Index Cond. Arm 1 uses migration 121's index. Arm 2
and the gap read use migration 159's new
`fact_records_content_entity_dependency_legacy_gap_repo_idx`, on
`((payload->>'repo_id'), scope_id, generation_id)` with a partial predicate
equal to those two readers' predicates joined by OR. The former
`$11 = '' OR` escape is removed. It was unreachable, because an empty `$11`
always comes with a target anchor that sets `$20`, and that skips the CTE.

Rejected shapes, measured on a scratch corpus of the same shape (803 scopes,
the target with 16 superseded generations):

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

Performance Evidence: Ops-qa figures were measured on ops-qa by a teammate on
2026-10-02, on PG 18.3, using read-only EXPLAIN (ANALYZE, BUFFERS):

- Base statement: ~9.4 s on the warm primary and over 30 s on the cold read
  replica (cancelled). About 97% of its buffers went to arm 2, which probed
  `fact_records_collector_status_active_idx` once per active scope (819 loops)
  and filtered ~2.7k rows from each.
- Arm 1 fetched 8,832 rows to keep 552 active ones.
- The gap read scanned the anchored repository's whole active scope (up to
  241,726 rows; 43 of 799 scopes have at least 10k active content_entity rows).
- Scope-pinned shapes without the new index ran in 61-177 ms warm. Cold, on
  repositories with 10k+ rows, they took 3.5-8.3 s, and the remaining cost was
  arm 2 and the gap read scanning the one active scope.
- With the `$11 = '' OR` escape kept, the statement timed out at 20 s under
  `force_generic_plan`. Without it, the arms ran in 79-191 ms.

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

The whole statement under the forced generic plan stays near 1.07M buffers
(1.30M before). That cost is in other CTEs (`package_consumption_correlation_active`
and the advisory family), which filter on `fact_kind = ANY($n)` and, on this
corpus, probe every active scope under a generic plan. #7088 does not change
them. The ops-qa generic-plan measurement above (79-191 ms without the
escape) suggests they are not a problem on that corpus. This is recorded as an
observation, not a claim.

The new index on the local corpus is 98,304 bytes, covering 1,621 of 281,816
`fact_records` rows. The seed deliberately puts one legacy and one gap row in
each of the 650 noise scopes. Migration 121's index is 245,760 bytes over
10,162 rows. On ops-qa the arm-2 predicate matches 0 rows and the gap
predicate matches 17 (teammate measurement), so the index is expected to be a
page or two. Write tax: only rows that satisfy the predicate pay index
maintenance. Every git content_entity insert or update pays the partial
predicate evaluation (a few JSONB extractions). No insert benchmark was run:
NOT_CHECKED.

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

The RED run (commit 4c405e8e2) used the origin/main SQL and schema. It failed
only on plan shape: 654 loops, the missing index, and generation not in the
Index Cond. Its row comparison and counts passed.

Commands:

```bash
ESHU_PACKAGE_MANIFEST_REPO_SCOPE_EXPLAIN_PROOF_DSN='postgres://postgres:…@127.0.0.1:<port>/postgres?sslmode=disable' \
ESHU_PACKAGE_MANIFEST_REPO_SCOPE_EXPLAIN_PROOF_DISPOSABLE=1 \
  go test ./internal/query/supply/chain/impact -run 'RepoArm|PackageManifestRepoScope' -count=1 -v
```
