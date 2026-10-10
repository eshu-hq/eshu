# QA migration 159 read-only run, 2026-10-03

This is the sanitized input record for the deployed after-index section of
`7088-readiness-repo-scope.md`. It contains no repository IDs, fact payloads,
API credentials, host addresses, or private machine paths. It is an
after-only observation, not a matched before/after speedup or a cold-cache
result.

## Binding and call shape

- API image observed on the `eshu-api` Deployment:
  `ghcr.io/eshu-hq/eshu:sha-101cd4f@sha256:ea66f880b2cb7ce2f3a93e4e269e892189c04c49f76dea25d8264c577fe980a2`.
  Source commit: `101cd4f6ab7b72ff01f018ad52112e3a9955168b`.
- The four files assembling `ListReadinessQuery`
  (`readiness_package_consumption_query.go`, `readiness_postgres_query.go`,
  `readiness_postgres_query_source.go`, and
  `readiness_postgres_query_select.go`) have no diff from that deployed
  commit to measurement-worktree base
  `dfcabe8679e7aaf373b48ae4f17d1355973ceecf` (`git diff --name-only`
  over those four paths printed nothing).
- PostgreSQL standby was in recovery. A temporary, uncommitted Go test used
  `database/sql` with pgx, a read-only repeatable-read transaction,
  `SET LOCAL statement_timeout='5s'`, and
  `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) ` + the shipped
  `ListReadinessQuery`. The 20 binds came from the existing
  `readinessArgsForRepository` test helper. The diagnostic source was
  removed after the run; its exact source is not a committed artifact.
- Anchors were selected by ordinal among active git repository scopes ordered
  by `right(scope_id,4),scope_id`. Ordinals 35, 141, 143, and 2 had earlier
  bounded active `content_entity` counts of approximately 1,113, 8,452,
  19,804, and 36,678 respectively. Identifiers are intentionally omitted.
- The API calls were serial `GET /api/v0/supply-chain/impact/findings` with
  `repository_id=<anchor>`, `limit=1`, `profile=precise`,
  `Accept: application/eshu.envelope+json`, a 2-second connect timeout,
  5-second total timeout, and no retry. Local port-forwards were bound to
  loopback and removed after the run. `time_total` is the HTTP client
  duration. The four anchors were the same as the plan probes. The reader
  continued WAL replay and cache state was warm or unknown.

## Full-statement plan transcript (sanitized)

All four plans used the migration-121
`fact_records_content_entity_dependency_variable_repo_idx` once and the
migration-159 `fact_records_content_entity_dependency_legacy_gap_repo_idx`
twice, once per legacy/gap arm. Each migration-159 scan had one actual loop,
two shared hits and zero shared reads. Root plan and migration-121 scan
figures from the run follow; `ms` is the JSON `Execution Time`.

| Ordinal | Full statement ms | Root shared hit/read | Migration-121 loop, hit/read | Migration-159 scans |
| ---: | ---: | ---: | ---: | ---: |
| 35 | 63.655 | 20,359 / 0 | 1, 258 / 0 | 2 scans, each 1 loop, 2 / 0 |
| 141 | 56.883 | 20,217 / 6 | 1, 198 / 6 | 2 scans, each 1 loop, 2 / 0 |
| 143 | 94.097 | 22,855 / 21 | 1, 2,686 / 21 | 2 scans, each 1 loop, 2 / 0 |
| 2 | 57.460 | 20,947 / 0 | 1, 625 / 0 | 2 scans, each 1 loop, 2 / 0 |

Sanitized Go test log lines underlying that table (file/line prefixes
removed; values and index names unchanged):

```text
ordinal=35 execution_ms=63.655 buffers_hit=20359 buffers_read=0 dependency_scans=3
ordinal=35 index=fact_records_content_entity_dependency_variable_repo_idx loops=1 hits=258 reads=0
ordinal=35 index=fact_records_content_entity_dependency_legacy_gap_repo_idx loops=1 hits=2 reads=0
ordinal=35 index=fact_records_content_entity_dependency_legacy_gap_repo_idx loops=1 hits=2 reads=0
ordinal=141 execution_ms=56.883 buffers_hit=20217 buffers_read=6 dependency_scans=3
ordinal=141 index=fact_records_content_entity_dependency_variable_repo_idx loops=1 hits=198 reads=6
ordinal=141 index=fact_records_content_entity_dependency_legacy_gap_repo_idx loops=1 hits=2 reads=0
ordinal=141 index=fact_records_content_entity_dependency_legacy_gap_repo_idx loops=1 hits=2 reads=0
ordinal=143 execution_ms=94.097 buffers_hit=22855 buffers_read=21 dependency_scans=3
ordinal=143 index=fact_records_content_entity_dependency_variable_repo_idx loops=1 hits=2686 reads=21
ordinal=143 index=fact_records_content_entity_dependency_legacy_gap_repo_idx loops=1 hits=2 reads=0
ordinal=143 index=fact_records_content_entity_dependency_legacy_gap_repo_idx loops=1 hits=2 reads=0
ordinal=2 execution_ms=57.460 buffers_hit=20947 buffers_read=0 dependency_scans=3
ordinal=2 index=fact_records_content_entity_dependency_variable_repo_idx loops=1 hits=625 reads=0
ordinal=2 index=fact_records_content_entity_dependency_legacy_gap_repo_idx loops=1 hits=2 reads=0
ordinal=2 index=fact_records_content_entity_dependency_legacy_gap_repo_idx loops=1 hits=2 reads=0
```

The follow-up run of the same diagnostic, augmented with an independent
`package.consumption` correlation count, again chose the same three index
probes at each ordinal. Its full-query times were 69.354, 50.673, 79.504,
and 57.143 ms respectively. All four independent consumption counts were
zero; that zero-row check alone does not establish general readiness truth.

## Enveloped HTTP `time_total` inputs

Each cell is one serial request in execution order, in seconds. All 32
returned HTTP 200, `truth.level=exact`, `truth.freshness.state=fresh`,
`readiness.freshness=fresh`, `readiness_state=evidence_incomplete`, and no
`error`. The canonical readiness-payload digest stayed identical across
the eight calls for each ordinal. The main note reports the median and
eight-sample nearest-rank p95 from these inputs.

| Ordinal | 1 | 2 | 3 | 4 | 5 | 6 | 7 | 8 |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 35 | 0.208204 | 0.204183 | 0.202642 | 0.204374 | 0.205583 | 0.208489 | 0.203565 | 0.226672 |
| 141 | 0.202025 | 0.201972 | 0.544880 | 0.202700 | 0.202491 | 0.204894 | 0.202235 | 0.223470 |
| 143 | 0.220789 | 0.222886 | 0.225392 | 0.221033 | 0.265203 | 0.231797 | 0.231173 | 0.221705 |
| 2 | 0.214650 | 0.210566 | 0.213116 | 0.210223 | 0.211095 | 0.214124 | 0.210336 | 0.208962 |

## Material-data check and storage observations

A separate read-only query counted active-generation git `content_entity`
Variable facts in one selected repository with `config_kind` among
`vcs_dependency`, `path_dependency`, `url_dependency`,
`editable_dependency`, and `unsupported_dependency`. It constrained
`scope.source_key = payload->>'repo_id'` and found five rows. The deployed
endpoint for that same repository returned five `dependency_source`
unsupported targets, HTTP 200, `readiness_state=unsupported`, and an
`exact`, `fresh` truth envelope in 0.489043 seconds. No ID was printed.

The schema Job logged
`bootstrap.postgres.migration.concurrent_index_build.finished`
`duration_ms=1221522` for migration 159, finishing at 03:08:09 UTC. The
standby's cumulative `confl_snapshot` count was seven afterwards; there is
no build-attributable before/during delta. Read-only size queries on the
standby returned 49,152 bytes for the migration-159 index and
208,274,554,880 bytes for the `fact_records` heap; the index's local
`pg_stat_user_indexes.idx_scan` counter was 1,049 without a comparator.

A bounded 45-repository candidate query for a 50k-70k-row anchor found
none. Its `EXPLAIN (ANALYZE, BUFFERS)` took 4,321.415 ms, with 66,776
shared hits and 19,227 shared reads. It was not widened into a fleet scan.

Cold first-call cache conditions, an approximately 59k-row anchor,
independent all-family truth, populated non-repository cases on the QA environment,
build-attributable replica conflicts, and deployed writer insert A/B remain
unmeasured. The corresponding issues remain open.
