# Supply-Chain Readiness Snapshot: Dependency-Gap Scope Bound (#7007)

## What was reported

After #7037 bounded the impact findings read, the live ops-qa API still logged
`supply_chain_query.stage_completed stage=readiness_snapshot
duration_seconds=7.457` while `impact_findings_query` took 0.023s. A
repository-anchored `GET /api/v0/supply-chain/impact/findings` took 5.6-8.8s
and returned zero findings.

## Where the time goes

Read-only `EXPLAIN (ANALYZE, BUFFERS)` of `ListReadinessQuery` on ops-qa
(PostgreSQL 18.3; the plan shows 814 ingestion scopes and 810 active scope
generations), through `PREPARE`/`EXECUTE` with the production
argument shape and a repository anchor:

- Execution 8119 ms. The `unsupported_target_rows` CTE alone is 8036 ms.
- Inside it, the `dependency_source` branch reads `package_dependency_gap_active`.
  That CTE filters `content_entity` facts by five provenance-only `config_kind`
  values (`vcs_dependency`, `path_dependency`, `url_dependency`,
  `editable_dependency`, `unsupported_dependency`) and, unlike
  `package_manifest_active` after #7037, carried no repository predicate.
- The plan probes `fact_records_collector_status_active_idx` once per active
  scope (`loops=810`), removing about 2.7k rows by filter each time
  (`Buffers: shared hit=692357 read=391781 written=36362`).
- Only 17 such facts exist fleet-wide, in 6 repositories, so the read returns
  almost nothing. No index leads with these kinds, so the read is a per-scope
  scan-and-filter.

Rejected: an advisory-family scan (ops-qa holds no vulnerability facts; the
standalone advisory scan is 5 ms) and lock or queue wait (a single read with
no lock or wait in the plan).

## Fix

`package_dependency_gap_active` now requires `$11 <> ''`,
`scope.source_key = $11` and `payload->>'repo_id' = $11`. Its only consumer
already required `$11 <> '' AND payload->>'repo_id' = $11`, so the rewrite is a
pure narrowing. `ingestion_scopes.source_key` is the repository id the git
collector stamps on every repository and repository_ref scope
(`buildScope`, pinned by `TestBuildScopeRepositorySourceKeyMatchesMetadataRepoID`).
On ops-qa a scan of all 17 active gap facts (active `fact_records` joined to
`ingestion_scopes` and `scope_generations`, counting `payload->>'repo_id' IS
DISTINCT FROM source_key`) returned `17|0`: none differs from its scope's
`source_key`.
No index or DDL is added.

Performance Evidence: ops-qa read-only, same argument shape, alternating
which variant runs first, three rounds over three repositories, execution
milliseconds (before / after):

| Repository | Round 1 | Round 2 | Round 3 |
| --- | --- | --- | --- |
| r_3127d45a | 7990 / 189 | 9839 / 202 | 8653 / 69 |
| r_90d01856 | 9718 / 107 | 5781 / 102 | 7069 / 144 |
| r_68f8cff0 | 10068 / 111 | 5361 / 90 | 8216 / 66 |

Median before 8216 ms, median after 107 ms (about 77x, or 8.1 s saved per
call). Caveat: ops-qa was draining a reprojection backlog during these runs,
so absolute numbers carry write-load noise; the before/after gap is far larger
than the spread. Plan cost estimates: custom plan 6554 -> 6081, forced generic
plan 8496 -> 8023. The generic plan costs more than the custom average, so
`plan_cache_mode=auto` does not switch to it; under a forced generic plan the
whole query still exceeds 120 s. That run was not repeated on the pre-fix
SQL, so the comparison is unmeasured; the fixed CTE is bounded in the generic
plan too (see Follow-up for the branch that is not).

Row-set equivalence: both variants were run (rows, not plans) for 12
repositories: six that carry gap facts (`unsupported_dependency` x5, x5 and
x3, `path_dependency` x2, `vcs_dependency` x1, `editable_dependency` x1)
and six from the reported calls. All 12 outputs are byte-identical after
sorting, including the non-empty `unsupported_targets_json` values.

Regression proof: `TestSupplyChainImpactReadinessPackageManifestRepoScopeQueryPlanLive`
seeds 600 noise repositories, each with a gap fact, plus a target repository
with two, and asserts the plan anchors `source_key` to the target repository
and that only the target's `dependency_source` rows are reported. Without the
predicate it fails on the plan-shape assertion; with it, it passes.

Observability Evidence: the existing `supply_chain_query.stage_completed
stage=readiness_snapshot` log and `duration_seconds` field (added in #7037)
measure this read; no signal changes. Expected after the change on ops-qa is
well under 1 s for repository-anchored calls.

## Follow-up (not in this change)

For a cve, package, subject-digest or image-ref anchor `$11` is empty, so
`package_manifest_dependency` (`WHERE ($11 = '' OR ...)`) counts every
dependency-variable fact in the fleet. On ops-qa each of those four anchors
exceeded a 120 s statement timeout, and a bare count of those facts exceeded
280 s. With that one branch gated on a
repository anchor, the same four anchors run in 108-154 ms. Gating changes
what `package.consumption` reports for those anchors, so it needs an owner
decision rather than riding with this equivalence-preserving fix.
