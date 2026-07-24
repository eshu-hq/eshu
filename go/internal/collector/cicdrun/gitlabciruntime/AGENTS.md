# AGENTS.md — cicdrun/gitlabciruntime guidance

## Read First

1. `README.md` — package purpose, provider contract, and safety rules.
2. `source.go` — claim-to-fact flow (`buildPipelineEnvelopes`, one fixture
   normalization per fetched pipeline) and runtime target validation
   (`validateTarget`, including the `defaultMaxRuns` fill).
3. `client.go` — GitLab REST pagination (`X-Total` header, not an in-body
   `total_count`), request bounding, and the `pipelinesPageTruncated`
   truncation signal.
4. `rate_limit.go` — GitLab's 429 + `RateLimit-ResetTime`/`Retry-After`
   throttling signal, a DIFFERENT status code and header pair than
   `../ghactionsruntime/rate_limit.go`'s GitHub 403/429 +
   `X-RateLimit-Reset` convention. Do not assume the two are interchangeable.
5. `source_telemetry.go` — tracing/metrics recording split out of `source.go`
   for the 500-line cap; reuses the SAME `eshu_dp_ci_cd_run_*` instruments
   `../ghactionsruntime` records to, labeled `provider=gitlab_ci`.
6. `../gitlab_ci_fixture.go` and `../types.go` (parent package) — the shared
   normalizer this runtime delegates fact construction to, and the exact
   `gitlabCIFixture`/`gitlabPipeline`/`gitlabJob`/`gitlabArtifact` JSON shape
   `buildPipelineEnvelopes` must marshal into.
7. `../AGENTS.md` — fixture normalizer boundary. Do not move live HTTP code
   into the parent package.

## Invariants

- Keep GitLab CI/CD provider polling in this runtime package, not in
  `internal/collector/cicdrun`.
- Fetch every pipeline in the bounded window (`max_runs`, default 10, hard
  cap 100), not just the newest one. `GitLabClient.FetchPipelines` fetches a
  bounded pipeline-list page, then a per-pipeline detail GET (the list
  endpoint omits `started_at`/`finished_at`/`user`) and a bounded jobs page
  (jobs carry artifacts inline — GitLab has no separate artifacts endpoint
  the way GitHub Actions does) for each pipeline in the window.
- An omitted/zero `max_runs` resolves to `defaultMaxRuns` (10) in
  `validateTarget`; only an explicit out-of-range value (negative, or above
  the hard cap) is rejected — mirrors `../ghactionsruntime`'s
  `defaultMaxRuns` rationale.
- Every pipeline's normalized facts are keyed by provider pipeline ID
  (`stable_fact_key`), independent of fetch/emission order and independent
  of `generation_id`, so re-fetching the same window on a later claim cycle
  is an idempotent upsert at projection.
- This package has NO cross-cycle run-watermark/gap-detection subsystem
  (the #5429 mechanism `../ghactionsruntime/run_watermark.go` and
  `pending_watermark.go` implement for GitHub Actions). This is a
  deliberate v1 scope decision (see `doc.go`), not an oversight — do not
  add it without first establishing GitLab pipeline volume actually
  produces the same cross-cycle backfill-gap risk GitHub Actions runs do.
- `PRIVATE-TOKEN`, not `Authorization: Bearer`, is GitLab's auth header —
  confirmed against GitLab's own REST API docs
  (https://docs.gitlab.com/api/rest/, https://docs.gitlab.com/api/pipelines/,
  https://docs.gitlab.com/api/jobs/). Do not "fix" this to match GitHub's
  convention.
- `TargetConfig.ProjectPath` is URL-path-escaped as ONE segment
  (`url.PathEscape`, which encodes `/` as `%2F`) when building request paths
  — GitLab's `:id` path parameter accepts a URL-encoded `namespace/project`
  path exactly this way. A GitLab project path may nest under any number of
  subgroups (unlike GitHub's fixed owner/repo), so `normalizeProjectPath`
  only requires at least one `/`, not exactly two segments.
- Credential-free tests only: every test in this package uses
  `httptest.NewTLSServer` with synthetic hosts/projects/tokens. Never commit
  a real GitLab host, project path, or token.
