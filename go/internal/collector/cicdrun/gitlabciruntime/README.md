# cicdrun/gitlabciruntime

Live GitLab CI/CD collector runtime: fetches bounded pipeline and job
metadata from GitLab's REST API and normalizes it into the SAME `ci.*`
reducer facts the offline cassette/fixture path and the GitHub Actions
runtime (`../ghactionsruntime`) already emit — GitLab is a second provider on
the existing `ci_cd_run` contract, not a parallel collector family.

## Why this package exists (#5427)

The `list_ci_cd_run_correlations`/`count_ci_cd_run_correlations` MCP surface
advertises `provider: gitlab_ci` as a valid filter value, and
`go/internal/collector/cicdrun/gitlab_ci_fixture.go` (`GitLabCIFixtureEnvelopes`)
normalizes GitLab pipeline+job payloads into facts — proven against the
golden-corpus cassette
(`testdata/cassettes/cicdrun/supply-chain-demo.json`'s
`ci_cd_run:gitlab_ci:eshu-hq:supply-chain-demo` scope). But before this
package existed, `go/cmd/collector-cicd-run/service.go` only ever constructed
`ghactionsruntime.NewClaimedSource` with `ghactionsruntime.GitHubClient` —
there was no code path that could fetch a REAL GitLab project's pipelines. A
production `ci_cd_run` collector instance configured with a
`provider: gitlab_ci` target could not actually collect anything: the
provider was advertised in the MCP schema and proven only against synthetic
golden facts, not servable from a live GitLab instance. This package closes
that gap.

## Architecture

```
GitLabClient.FetchPipelines(ctx, TargetConfig)
  -> GET /projects/:id/pipelines           (bounded list, newest first)
  -> GET /projects/:id/pipelines/:id       (per-pipeline detail: started_at,
                                             finished_at, user — the list
                                             endpoint omits these)
  -> GET /projects/:id/pipelines/:id/jobs  (bounded job list, artifacts
                                             reported inline on each job)
       |
       v
ClaimedSource.NextClaimed
  -> marshals each PipelineSnapshot into {"pipeline":..., "jobs":...,
     "jobs_partial":...}
  -> cicdrun.GitLabCIFixtureEnvelopes(raw, FixtureContext)   (SHARED with the
     offline fixture path — one normalizer, two entry points)
  -> collector.FactsFromSlice(scope, generation, envelopes)
```

## GitLab API contract (researched against docs.gitlab.com, not guessed)

- Base path: `/api/v4` (default `https://gitlab.com/api/v4`; self-hosted
  instances configure `api_base_url`).
- Auth: `PRIVATE-TOKEN: <token>` request header (GitLab's REST API, not
  GitHub's `Authorization: Bearer` convention).
- Pagination: `page`/`per_page` query parameters (offset-based); the
  `X-Total` response header reports the total item count when GitLab
  provides it (GitLab.com may omit some pagination headers for some
  endpoints, so the truncation check falls back to "the page exactly filled
  the requested window" the same way `../ghactionsruntime`'s GitHub client
  does when `total_count` is absent).
- Rate limiting: HTTP 429 with `RateLimit-ResetTime` (RFC1123 date) and/or
  `Retry-After` (seconds) response headers.
- Job artifacts are reported INLINE on each job object (`file_type`, `size`,
  `filename`, `file_format`) with no content digest at this level — GitLab's
  Jobs API has no separate per-artifact digest field the way GitHub Actions'
  Artifacts API does, so every normalized GitLab artifact fact carries an
  empty `artifact_digest` plus an `artifact_missing_digest` warning fact
  (see `../gitlab_ci_fixture.go`), matching the real API shape rather than a
  fixture gap.

Sources: https://docs.gitlab.com/api/pipelines/, https://docs.gitlab.com/api/jobs/,
https://docs.gitlab.com/api/rest/ (pagination),
https://docs.gitlab.com/administration/settings/user_and_ip_rate_limits/ (rate limit headers).

## Deliberately out of scope for v1

Mirrors `../gitlab_ci_fixture.go`'s own documented narrower scope:

- No cross-cycle run-watermark/gap detection (the #5429 mechanism
  `../ghactionsruntime` implements for GitHub Actions runs). GitLab
  pipelines are a lower, steadier per-project volume than GitHub Actions
  runs in the corpora this collector currently targets; add this only after
  establishing the same cross-cycle backfill-gap risk applies.
- No `ci.pipeline_definition`, `ci.step`, `ci.trigger_edge`, or
  `ci.environment_observation` facts — GitLab's Pipelines/Jobs APIs expose
  no matching resources at this level (see `../gitlab_ci_fixture.go`'s doc
  comment for the full rationale).

## Safety

- Every test is credential-free: `httptest.NewTLSServer` with synthetic
  hosts, project paths, and tokens. Never commit a real GitLab host, project,
  or token.
- HTTP error messages never echo request tokens or the raw provider response
  body (see `TestGitLabClientReturnsBoundedSDKHTTPErrorForPermissionFailure`).
