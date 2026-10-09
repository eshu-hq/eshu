# GitLab Group Discovery: Proof And Rollout (#7765)

Status: proposed. Companion to
[the design page](7765-gitlab-group-discovery.md), which holds the decision and
the contracts. This page holds the prove-first plan, the test plan and the PR
breakdown. Source check: origin/main 7be568e44, 2026-10-09.

## Prove the theory first (before PR 1 code)

**Hypothesis.** The prefix predicate costs about the same as the first-segment
read. That read was measured as a seq scan of 594 buffers in 4.8-5.4 ms at
12,000 git scopes (`storage/postgres/membership/README.md:77-94`).

**Shim.** A throwaway SQL script on a disposable PostgreSQL 18 with migrations
001 and 091 indexes. The seed and query scripts are committed with PR 1 as an
evidence note (see "Prove-first results"). The seed:

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
predicate, as specified in the design.

The result is a pass on the adjudicating measure, `pgbench` latency plus
container CPU, with a thin margin. It is not a clean pass on every measure: the
`EXPLAIN (ANALYZE)` medians crossed the bars in two plan modes (see "EXPLAIN-mode
medians" below). At 50,000 scopes the prefix query is 1.54x the slug-only query
with parallelism off, slightly over the 1.5x bar. That bar was specified at
12,000 scopes only, so it is not a failure there, but it is reported instead of
hidden.

**Environment.**
- PostgreSQL 18.6, image
  `postgres@sha256:74935e72241653ca55e0414067e6d8763aceb8a810eb51b452253ec3dcfc4336`,
  default settings (`shared_buffers` 128MB, `en_US.utf8` collation, JIT on,
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

The 985 buffers above do not match the 594 buffers the store README records for
a slug-only read at the same 12,000 git scopes. The shim's payload carries six
keys (`repo_id`, `repo_name`, `source_key`, `repo_slug`, `remote_url`,
`local_path`), so its heap is wider. The README's payload width was not checked,
so the width gap is the likely, unverified, cause. Only same-session
comparisons count, and the absolute times here are not comparable with the
README's 4.8-5.4 ms.

**EXPLAIN-mode medians (12,000 scopes).** From
`interleaved_12k.summary.txt`, the prefix query against slug-only, same session:

| Plan mode | Statistic | Prefix | Slug-only | Ratio | Against the bars |
|---|---|---|---|---|---|
| auto | 5-warm median | 8.065 ms | 5.913 ms | 1.36x | within |
| auto | 11-warm median | 6.772 ms | 4.788 ms | 1.41x | within |
| forced generic | 5-warm median | 8.347 ms | 4.780 ms | 1.75x | over 1.5x |
| forced generic | 11-warm median | 8.571 ms | 6.736 ms | 1.27x | within |
| forced custom | 11-warm median | 10.321 ms | 8.226 ms | 1.25x | over 10 ms |

The forced-generic 1.75x and the forced-custom 10.32 ms exceed the bars, so
they are reported here. `EXPLAIN ANALYZE` wall time was noisy under host load:
the slug-only forced-generic median moved from 4.780 ms (5 warm) to 6.736 ms (11
warm) on the same statement, and single runs ranged from 4.5 ms to 7.3 ms.
`pgbench -M prepared` plus container CPU adjudicates for that reason. It runs 200
executions per round over nine rounds with the variants alternating, so a load
spike lands on every variant. Container CPU counts CPU consumed, not time spent
waiting to be scheduled. Neither measure proves the driver's plan mode, which
is NOT_CHECKED.

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
stays under 10 ms at 50,000 rows (serial medians 18-29 ms), including the query
`github_org` runs today, so the 10 ms bar cannot hold for any query at that
size.

**Cause of the CPU gap.** Buffers do not explain it, because the prefix query
reads fewer. The extra time is CPU in `lower()` over the full slug. Without
`lower()` the prefix query is 0.80x slug-only at 12,000 rows and 0.83x at 50,000
(serial). Collation was not varied, so whether it changes the `lower()` cost is
untested. Dropping it relies on `repo_slug` always
being lowercase, which holds today: `NormalizeRemoteURL` lowercases the slug
and `buildScope` is the only writer. That is an invariant of the writer, not of
the schema.

**Recommendation.** Keep the prefix predicate and `lower()` as specified. It keeps the match
case-insensitive without depending on a writer invariant. The
owner may drop it as a documented option if the 50,000-scope margin matters; the
change must then state the lowercase-slug invariant next to the constant. No
index is justified on the adjudicating measure, so the index candidates were not
triggered.

**Before PR 1 merges, re-measure on a quiet host.** The EXPLAIN-mode excursions
above came from a loaded host, so they neither fail nor clear the bars. PR 1
repeats the 12,000-scope run on a quiet host, reports `pgbench` latency,
container CPU and `EXPLAIN` medians together in the evidence note and the store
README, and runs the "On failure" index measurement if any bar is still crossed
in a plan mode the driver can use.

**Go side.** `BenchmarkEvaluateQAFixtureSteadyState` with a scratch
`partitionKnown` prefix variant, six alternating pairs: the baseline median is
251.7 µs/op and the prefix median is 258.5 µs/op (+2.7%, bar 10%), both at 19
allocs/op. Absolute times are above the recorded 181-183 µs because the host was
loaded, so only the same-session comparison counts.

**NOT_CHECKED.**
- Index candidates (not triggered).
- Absolute time on a quiet host (PR 1 re-measures it).
- Driver plan-cache behavior.
- Real QA slug lengths.
- Cold cache.
- Any collation other than `en_US.utf8`, and whether collation changes the
  `lower()` cost.
- The README's payload width (the 985 vs 594 buffer gap).
- `pg_stat_statements`.
- Concurrent-write contention at 50,000 scopes.
- The migration 092 and 130 triggers.

The seed and query scripts and the raw outputs are committed with PR 1 as an
evidence note, `docs/internal/evidence/7765-gitlab-group-prefix-read.md`. The
store README gets the "Performance Evidence (#7765)" section in PR 1. Until
then, the figures on this page are the only record.

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
  `evaluate_test.go`/`observer_test.go` assertion stays unchanged. Only the
  `KnownScopes` signature in the fakes and direct callers is updated, as a
  mechanical edit listed under PR 1.
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
- Bootstrap twice to prove migration 169 is idempotent.
- The migration checksum manifest and golden digest pins (see PR 2).
- Enrol the live test in the live-postgres-readiness ledger
  (`specs/live-tests.v1.yaml`).

**Git package.**
- `TestGitLabGroupSelectionRequestMapsScopeIdentity`: scope IDs and slugs equal
  `gitScopeIdentityForRepositoryID`, and the host is `gitlab.com`.
- `observeSelection` observes `gitlabGroup` on shard 0 only.
- Config rejections: missing group, `githubApp`, `ssh`, `GITHUB_TOKEN`-only, and
  a path-form group with an exact rule outside its prefix.
- `TestDiscoverSelectionGitLabGroupRejectsExactRuleOutsideResolvedGroup`: with
  `ESHU_GITLAB_GROUP=42` (numeric) config load succeeds. A fixture
  `GET /groups/42` returns `full_path="acme/platform"` and an exact rule
  `gitlab/acme/other/x` fails the cycle with `failure_class=config_invalid`,
  before any `/projects` request and with no observation rows. The same rule
  under `gitlab/acme/platform/` passes.
- `TestWebhookSelectorGitLabGroupDropsNonGitLabTriggers`: with
  `SourceMode=gitlabGroup`, GitHub and Bitbucket triggers are marked failed with
  `provider_not_selected` and never reach `SyncGit`, and a GitLab trigger in the
  same batch still syncs as `gitlab/<path>`. In `githubOrg` mode the same
  GitHub trigger syncs as before.
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

1. **Partition generalization** (needs the "Prove the theory first" proof, and
   a quiet-host re-measurement before merge, see "Prove-first results").
   - Files: `membership/{evaluate,identity,observer}.go` (`Partition` and the
     interface), `storage/postgres/membership/observations.go` (prefix
     constant), both READMEs, and
     `docs/internal/evidence/7765-gitlab-group-prefix-read.md` (the seed and
     query scripts plus raw outputs).
   - The mechanical `KnownScopes` signature update to `(ctx, Partition)`, with
     no assertion changes:
     - fakes: `membership/observer_test.go:45`,
       `git/selection_liveness_test.go:52`,
       `git/selection_observation_explicit_test.go:172`,
       `git/selection_observation_test.go:155`;
     - direct callers: `storage/postgres/membership/observations_test.go:34`,
       `:236`, `:248`, `known_scopes_live_test.go:37` and
       `observations_live_test.go:63`.
   - Gates: go-fmt/lint/vet/build, go-file-cap, go-dir-gate, package-docs,
     perf-evidence, go-test-race, live-postgres-readiness, telemetry-coverage
     (no change), markdown-file-cap, measurement-citations.
   - No new kind is reachable yet.
2. **`gitlab_group` kind.**
   - Files: `KindGitLabGroup`, `NewGitLabGroupSelector`, `Selector.Host`,
     migration 169, the `validateBatch` change, the telemetry constant, the
     gauge condition, telemetry docs.
   - Migration 169 is also appended to two pins in
     `go/internal/storage/postgres/migrations`. Add its line to
     `migrationShippedChecksums` in `migration_checksum_manifest_test.go`,
     copying the checksum that the test reports for the new file, and never edit
     an existing line. Recompute `goldenBootstrapDefinitionsDigest` and bump
     `goldenBootstrapDefinitionsCount` (187 today) in `embed_invariant_test.go`,
     with a comment line naming #7765, as the recent migrations did.
   - Gates: PR 1's gates plus migration-immutability (new file), the
     checksum manifest and golden digest tests, and the telemetry-coverage row.
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
     liveness window's modes), `webhook_trigger_selector.go` (the
     `provider_not_selected` guard), and the mode mentions in
     `go/cmd/ingester/doc.go:35` and `go/cmd/collector-git/doc.go:18`.
   - Gates: env-registry-doc, docs-cli-env-refs, telemetry-coverage,
     perf-evidence, docs-build-changed.
5. **Helm and docs.**
   - Files: `values.yaml`, `values.schema.json`, `statefulset.yaml`,
     `_validation_core.tpl`, the helm-runtime-values and compose docs.
   - Gates: helm-package, docs gates.

## Rollout surfaces

**Env registry.**
- Add `ESHU_GITLAB_GROUP` (VarString, subsystem `collector`) to
  `go/internal/envregistry/entries.go`, beside
  `ESHU_REPO_SELECTION_LIVENESS_WINDOW` (`:117`).
- Add `ESHU_REPO_SOURCE_MODE` as a VarEnum
  (`githubOrg|explicit|filesystem|gitlabGroup`, default `githubOrg`), so a typo
  fails `eshu config validate`.
- `selection_config.go` is not in `coreScanFiles` (`coverage_test.go:19-50`), so
  this is declarative. It is still needed because `docs-cli-env-refs` treats an
  unregistered variable in docs as new debt.
- Regenerate `docs/public/reference/env-registry.md`.

**Helm.**
- `values.yaml:1177-1184`: add `repoSync.source.gitlabGroup: ""`.
- `values.schema.json:1323-1326`: add `gitlabGroup` to the mode enum.
- `statefulset.yaml:85-100`: render `ESHU_GITLAB_GROUP`.
- `_validation_core.tpl:67-69`: keep the ssh rule's wording ("ssh requires
  explicit or filesystem"). Add two fails:
  - `mode=gitlabGroup` requires `auth.method=token`, so `githubApp` and `ssh`
    both fail with a message naming the mode;
  - `mode=gitlabGroup` requires a non-empty `source.gitlabGroup`.
- Update `docs/public/deploy/kubernetes/helm-runtime-values.md:212-216`.

**Compose.** `docker-compose.yaml:181-184` hardcodes `ESHU_GIT_AUTH_METHOD: none`
and never supported `githubOrg`, so no compose file changes.
`docs/public/run-locally/docker-compose.md` gets one paragraph: `gitlabGroup`
needs a token and an override file, like `githubOrg`.

## Telemetry

- **Counter.** `selector_kind="gitlab_group"` on
  `eshu_dp_collector_repository_selection_evaluations_total`. Add the constant
  `RepositorySelectionSelectorKindGitLabGroup` beside the existing two
  (`instruments_repository_selection.go:35-42`) and update the description
  (`:72`) and `contract.go:73`.
- **Gauge.** `eshu_dp_collector_repository_selection_scopes` has **no
  `selector_kind` label** (`:76-81`, `observer.go:184-187`). Extend the
  condition at `observer.go:174` to sample `gitlab_group` evaluated cycles too.
  One collector process runs one source mode, so no two kinds share a series.
  No label is added, because that would be a metric contract change. The
  description becomes "githubOrg or gitlabGroup selector".
- **New counter** `eshu_dp_collector_repository_listing_requests_total{collector_kind="git",provider="gitlab",outcome}`.
  - Outcomes are closed: `success`, `retried`, `rate_limited`, `http_error`,
    `transport_error`, `decode_error`.
  - It lets an operator see throttling without logs. The GitHub listing is
    unchanged here (follow-up).
- **Logs.**
  - INFO `git_gitlab_group_listing_completed`: `group_path`, `pages`,
    `listed_count`, `outside_group_count`, `listing_complete`, `duration_seconds`.
  - WARN `git_gitlab_group_listing_retry`: `status_code`, `attempt`,
    `retry_after_seconds`.
  - Failures are returned with `failure_class` (see "GitLab listing client" on the design page).
  - `selector_kind` already rides on every `git_repository_selection_*` log
    (`observer.go:198`).
- **Spans.** No new span. Selection runs inside `scope.assign` and
  `eshu_dp_scope_assign_duration_seconds` (`source_processing.go:32-48`).
- **Status.** The freshness `not_selected` verdict reads every live row by scope
  ID, whatever its kind (`storage/postgres/membership/live_read.go:22-29`), so
  it needs no change.
- **Docs, in the same PR.** `docs/public/reference/telemetry/metrics-ingestion-collectors.md:34`
  (465 lines, so split it if the new row crosses 500), `metrics.md:181`, the
  discovery row in `docs/public/observability/telemetry-coverage.md:622` (the
  `telemetry-coverage` gate), and both membership READMEs.
