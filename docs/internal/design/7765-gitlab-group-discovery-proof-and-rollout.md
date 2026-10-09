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

Pending: results are added before this PR is published.

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
