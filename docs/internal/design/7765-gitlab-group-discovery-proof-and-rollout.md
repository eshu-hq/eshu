# GitLab Group Discovery: Proof And Rollout (#7765)

Status: proposed. Companion to
[the design page](7765-gitlab-group-discovery.md), which holds the decision and
the contracts. This page holds the prove-first plan, the test plan and the PR
breakdown. Source check: origin/main 195337b97, 2026-10-08.

## Prove the theory first (before PR 1 code)

**Hypothesis.** The prefix predicate costs about the same as the first-segment
read. That read was measured as a seq scan of 594 buffers in 4.8-5.4 ms at
12,000 git scopes (`storage/postgres/membership/README.md:77-94`).

**Shim.** A throwaway SQL script, not committed, on a disposable PostgreSQL 18
with migrations 001 and 091 indexes:

- 20,000 `ingestion_scopes` rows, 12,000 of them git repository scopes;
- gitlab.com `acme/platform/**` 1,000 (depth 3-6), `acme/other/**` 500,
  `acme/platformer/**` 50 and `acme_platform/**` 20;
- github.com `acme/*` 900, plus 200 other owners across three hosts;
- 300 `repository_ref` scopes under `acme/platform`.

Run the exact constants as prepared statements: the prefix query, the current
slug-only query and the host-filtered query. Use
`EXPLAIN (ANALYZE, BUFFERS)` with five warm runs each, and execute six or more
times to capture the generic plan as well.

**Pass criteria (all required).**
- The row set is exactly the 1,000 nested scopes. No `platformer`, no
  `acme_platform`, no ref scopes.
- Median execution ≤ 1.5× the slug-only median on the same data, and ≤ 10 ms.
- Shared buffers ≤ slug-only + 5%.
- The generic plan has the same shape.
- No tuple locks, only `AccessShareLock`, as before.
- Repeat at 50,000 git scopes to show linear growth.
- Go side: a `BenchmarkEvaluate` prefix variant on the 802-scope QA fixture
  stays within 10% of the recorded 181-183 µs/op.

**On failure.** Measure two index candidates before choosing either:

- (a) A partial expression btree:
  `((lower(payload->>'repo_slug')) COLLATE "C") WHERE source_system='git' AND scope_kind='repository' AND collector_kind='git'`,
  with a range rewrite: `>= $1 AND < $1 || chr(1114111)`.
- (b) An SP-GiST index serving `^@`.

Either is accepted only if the read improves by at least 2× **and** the
`ingestion_scopes` upsert p50 regresses by less than 5%. That table is written
on every generation. Record the numbers in the store README as
"Performance Evidence (#7765)".

## Prove-first results

The shim ran on 2026-10-08, and every figure below comes from that run. The
prefix query is `starts_with(lower(payload->>'repo_slug'), $1)` plus the host
predicate, as specified in the design. The 12,000-scope run passes every
criterion, but the CPU margin is thin. At 50,000 scopes the prefix query is
1.54x the slug-only query with parallelism off, slightly over the 1.5x bar.
That bar was specified at 12,000 scopes only, so it is not a failure, but it is
reported here instead of hidden.

**Environment.**
- PostgreSQL 18.6, image
  `postgres@sha256:74935e72241653ca55e0414067e6d8763aceb8a810eb51b452253ec3dcfc4336`,
  default settings (`shared_buffers` 128MB, `en_US.utf8`, JIT on,
  `max_parallel_workers_per_gather` 2).
- Apple M5, 10 CPUs, 32 GiB, Docker 28.5.2, `linux/arm64`.
- origin/main 195337b97 with migrations 001 and 091 applied verbatim.
- 20,000 rows (12,000 git repository scopes) and 58,000 rows (50,000),
  seeded per the shim above with a shuffled heap.

**Method.** Prepared statements under `EXPLAIN (ANALYZE, BUFFERS)`, with 12
executions each in auto, forced-generic and forced-custom plan modes.
Timing used `pgbench -M prepared` and container CPU (`cpu.stat` usage per
execution) over nine rounds of 200 executions, variants alternating. A first
pass at host load 100-200 was discarded. The figures below are from load 6-10.

**Results at 12,000 git scopes.**

| Criterion | Bar | Result | Verdict |
|---|---|---|---|
| Row set | exactly the 1,000 nested scopes | 1,000 rows, 0 missing, 0 unexpected, 0 ref scopes, depths 3-6 at 250 each | PASS |
| Median vs slug-only | at most 1.5x | 1.34-1.41x (7.04 ms vs 5.26 ms container CPU) | PASS, thin margin |
| Absolute time | at most 10 ms | 6.9-8.1 ms | PASS |
| Buffers | at most slug-only + 5% | 853 vs 985 for slug-only custom (-13.4%) | PASS |
| Plan shape | same as the generic plan | Bitmap Heap Scan over `ingestion_scopes_source_idx`, the same shape as the host-filtered query | PASS |
| Locks | `AccessShareLock` only | 6 relation locks, 0 tuple locks, 0 non-AccessShare | PASS |

The prefix query is 1.22x the host-filtered query that `github_org` runs today
(7.04 ms vs 5.77 ms). The slug-only custom plan is a Seq Scan and near-ties the
bitmap plan, and auto mode never promoted any statement to a generic plan.

Two controls held. The same prefix with host `github.com` returns 0 rows, and a
peer session holding `FOR UPDATE` on a matching scope did not block the
prefix query (8.8 ms at 12,000, 17.0 ms at 50,000, with no tuple locks).

**Results at 50,000 git scopes.** The row set is identical to the 12,000 run
and the locks are unchanged. With parallelism on, the planner chose a parallel
seq scan for the host-filtered and prefix queries (13.8 ms prefix, 17.5 ms
slug-only in auto mode), which makes ratios against the serial slug-only plan
misleading. The apples-to-apples comparison is `max_parallel_workers_per_gather = 0`,
where all variants run the same serial scan:

| Variant | Latency (ms) | Ratio to prefix |
|---|---|---|
| slug-only | 18.98 | prefix is 1.54x |
| host-filtered, github.com | 18.17 | prefix is 1.61x |
| host-filtered, gitlab.com | 19.61 | prefix is 1.49x |
| prefix, gitlab.com | 29.18 | |

Latency is the `pgbench` per-statement median over seven rounds. Time grew
4.2x for 4.17x the rows (6.92 ms to 29.18 ms), so growth is linear. No variant
reaches 10 ms at 50,000 rows, including the query `github_org` runs today, so
the 10 ms bar does not hold for any query at that size.

**Cause of the CPU gap.** Buffers do not explain it, because the prefix query
reads fewer. The extra time is CPU in `lower()` over the full slug under
`en_US.utf8`. Without `lower()` the prefix query is 0.80x slug-only at 12,000
rows and 0.83x at 50,000 (serial). Dropping it relies on `repo_slug` always
being lowercase, which holds today: `NormalizeRemoteURL` lowercases the slug
and `buildScope` is the only writer. That is an invariant of the writer, not of
the schema.

**Recommendation.** Keep `lower()` as specified. It keeps the match
case-insensitive without depending on a writer invariant. The
owner may drop it as a documented option if the 50,000-scope margin matters; the
change must then state the lowercase-slug invariant next to the constant. No
index is justified. The index candidates were not triggered, because no
criterion failed at the bar's size.

**Go side.** `BenchmarkEvaluateQAFixtureSteadyState` with a scratch
`partitionKnown` prefix variant, six alternating pairs: the baseline median is
251.7 µs/op and the prefix median is 258.5 µs/op (+2.7%, bar 10%), both at 19
allocs/op. Absolute times are above the recorded 181-183 µs because the host was
loaded, so only the same-session comparison counts.

**NOT_CHECKED.**
- Index candidates (not triggered).
- Absolute time on a quiet host.
- Driver plan-cache behavior.
- Real QA slug lengths.
- Cold cache.
- Collations other than `en_US.utf8`.
- `pg_stat_statements`.
- Concurrent-write contention at 50,000 scopes.
- The migration 092 and 130 triggers.

The raw outputs are in the proof agent's scratch directory, not committed. The
store README gets the "Performance Evidence (#7765)" section in PR 1.

## Test plan (red first)

**Leaf `gitlab` (httptest fixture, new tests).**
1. **Query, headers, path.** The request carries `include_subgroups=true`,
   `with_shared=false`, `order_by=id`, `sort=asc`, `per_page=100`, no
   `archived`, no `min_access_level`, a `PRIVATE-TOKEN` header, and
   `r.URL.EscapedPath()` containing `acme%2Fplatform`.
2. **Paging.** 250 projects over pages 1-3, then an empty page with no Link:
   `Complete=true`, IDs ascending.
3. **Short page.** A short page mid-listing does not end it (mirrors
   `TestListGitHubOrgRepositoriesPagesAtAFixedPerPage`).
4. **Incomplete end.** An empty page that still carries `rel="next"`:
   `Complete=false`.
5. **Truncation.** Limit 150 of 250 gives 150 records and `Complete=false`. A
   limit equal to the total is also `Complete=false`.
6. **Subgroup depth.** `acme/platform/a/b/c/svc` is listed as `gitlab/...`.
7. **Outside the group.** A project `other/x` in the response is dropped and
   `OutsideGroup=1`.
8. **Archived.** The flag is decoded. Through `selectGitHubRepositoryIDs`:
   archived is excluded, or included with the include flag or an exact rule.
9. **Rate limit recovers.** `429 Retry-After: 1` then `200` succeeds with one
   sleep. HTTP-date form is parsed, and 3600 is clamped to 60s.
10. **Rate limit exhausted.** Four 429s give `rate_limited` and no records.
    Context cancellation during the wait returns `ctx.Err()`.
11. **Status classes.** 401, 403, 404, 500 and malformed JSON map to their
    classes with no partial result.
12. **Group resolve.** `GET /groups/Acme%2FPlatform` returns
    `full_path="acme/platform"`. A 404 gives `group_not_found`.

**Membership (partition and kind).**
- `TestEvaluateGitLabGroupJudgesOnlyTheGroupPrefix`:
  - known: `acme/platform/a`, `acme/platform/sub/b`, `acme/other/c`,
    `acme/platformer/d`;
  - owner `acme/platform`, listing only `a`;
  - rows: `a` selected, `sub/b` not_listed; `c` and `d` have no rows.
- `TestFirstSegmentPartitionMassExcludesNestedGroups`: this pins the ruling's
  hazard. With a first-segment partition and owner `acme`, `c` and `d` read
  `not_listed`, and with owner `acme/platform` nothing is known. It shows why
  `gitlab_group` must not use the first-segment mode.
- `TestPartitionFirstSegmentUnchanged`: a table over the existing fixtures
  (`evaluate_test.go:234` and onward, `observer_host_test.go`). Every existing
  `evaluate_test.go`/`observer_test.go` case passes unedited.
- `TestSelectorIDsArePinned`: literal IDs for one `github_org` and one
  `explicit` configuration, captured on main in PR 1.
- `TestNewGitLabGroupSelector`: the ID varies with host, path, rules, archived
  and principal, and the owner is lowercased.
- Observer: a blank host gives `store_error`, and the gauge is sampled for
  `gitlab_group`.

**Store.**
- A unit test pins the `KnownScopesQuery` text.
- A prefix-query shape test.
- `validateBatch` accepts `gitlab_group` not-listed rows.
- New live test `TestKnownScopesPathPrefixLive`, using the seeds from "Prove the theory first"
  including `acme_platform/*` and ref scopes, plus a `gitlab_group` upsert
  round trip.
- Bootstrap twice to prove migration 167 is idempotent.
- Enrol the live test in the live-postgres-readiness ledger
  (`specs/live-tests.v1.yaml`).

**Git package.**
- `TestGitLabGroupSelectionRequestMapsScopeIdentity`: scope IDs and slugs equal
  `gitScopeIdentityForRepositoryID`, and the host is `gitlab.com`.
- `observeSelection` observes `gitlabGroup` on shard 0 only.
- Config rejections: missing group, `githubApp`, `ssh`, `GITHUB_TOKEN`-only, and
  an exact rule outside the group prefix.
- `BenchmarkGitLabGroupSelectionRequest` at 800 projects, compared with
  `BenchmarkGitHubOrgSelectionRequest` at 3.27-3.36 ms/op.

**Helm.** No in-repo Helm template test harness exists, so run `helm template`
locally:
- fails for `gitlabGroup`+`ssh`, `gitlabGroup`+`githubApp`, and an empty group;
- renders `ESHU_GITLAB_GROUP` for a valid `token` configuration.

## PR breakdown

Every PR runs focused tests, `eshu-code-review`, the review-attest receipt and
`make pre-push`, and carries `Performance Evidence:` or
`No-Regression Evidence:` plus `Observability Evidence:` or
`No-Observability-Change:` markers for `perf-evidence`.

1. **Partition generalization** (needs the "Prove the theory first" proof).
   - Files: `membership/{evaluate,identity,observer}.go` (`Partition` and the
     interface), `storage/postgres/membership/observations.go` (prefix
     constant), tests, both READMEs.
   - Gates: go-fmt/lint/vet/build, go-file-cap, go-dir-gate, package-docs,
     perf-evidence, go-test-race, live-postgres-readiness, telemetry-coverage
     (no change), markdown-file-cap, measurement-citations.
   - No new kind is reachable yet.
2. **`gitlab_group` kind.**
   - Files: `KindGitLabGroup`, `NewGitLabGroupSelector`, `Selector.Host`,
     migration 167, the `validateBatch` change, the telemetry constant, the
     gauge condition, telemetry docs.
   - Gates: PR 1's gates plus migration-immutability (new file) and the
     telemetry-coverage row.
3. **Leaf `gitlab` client.**
   - Files: the new package (doc, README, AGENTS), the listing counter, the
     httptest suite.
   - Gates: package-docs, go-dir-gate (new directory under the cap),
     gosec-changed, telemetry-coverage, perf-evidence.
4. **Wiring and config.** Merge only after #7821.
   - Files: `selection_github.go`, `selection_discovery.go`,
     `selection_native.go`, `selection_cli.go`, `selection_config.go`, the env
     registry plus the regenerated `env-registry.md`, and
     `environment-ingestion-queues.md:11` and `:38` (the mode list and the
     liveness window's modes).
   - Gates: env-registry-doc, docs-cli-env-refs, telemetry-coverage,
     perf-evidence, docs-build-changed.
5. **Helm and docs.**
   - Files: `values.yaml`, `values.schema.json`, `statefulset.yaml`,
     `_validation_core.tpl`, the helm-runtime-values and compose docs.
   - Gates: helm-package, docs gates.
