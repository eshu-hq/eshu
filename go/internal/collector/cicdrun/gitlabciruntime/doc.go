// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package gitlabciruntime collects bounded GitLab CI/CD pipeline metadata for
// the CI/CD run collector family.
//
// The package owns hosted provider polling and claim resolution. It delegates
// fact envelope construction to the shared cicdrun.GitLabCIFixtureEnvelopes
// normalizer (go/internal/collector/cicdrun/gitlab_ci_fixture.go) so live
// GitLab rows and offline fixtures share one schema and emit the SAME ci.*
// fact kinds and reducer join-key shape (provider, run_id, run_attempt) that
// ghactionsruntime already produces for GitHub Actions -- GitLab is a second
// provider on the existing contract, not a parallel runtime family. HTTP
// responses are closed by the client after each bounded provider read. The
// package does not read artifact contents, logs, secrets, graph state, or
// query state.
//
// Every claim fetches a bounded, stateless window of the most recent
// pipelines (max_runs) plus each pipeline's jobs (max_jobs, GitLab reports
// job artifacts inline on the job so no separate artifact fetch or bound is
// needed); re-fetching the same window on a later cycle is an idempotent
// upsert at projection, not a resume operation.
//
// Scope is intentionally narrower than ghactionsruntime, matching the shared
// normalizer's own documented v1 scope: no cross-cycle run-watermark/gap
// detection (the #5429 mechanism ghactionsruntime's run_watermark.go and
// pending_watermark.go implement) -- GitLab pipelines are dense and typically
// low-volume per project relative to GitHub Actions runs, so this is
// deliberately deferred rather than mirrored blindly; see
// go/internal/collector/cicdrun/gitlab_ci_fixture.go's doc comment for the
// full narrower-scope rationale (no ci.pipeline_definition, no ci.step, no
// ci.trigger_edge/ci.environment_observation).
//
// GitLab's REST API (https://docs.gitlab.com/ee/api/pipelines.html,
// https://docs.gitlab.com/ee/api/jobs.html) differs from GitHub's in three
// ways this package accounts for: authentication uses a PRIVATE-TOKEN header
// rather than a Bearer Authorization header; pagination totals are reported
// via the X-Total response header rather than an in-body total_count field;
// and rate-limit retry guidance is reported via RateLimit-ResetTime /
// Retry-After response headers on a 429 rather than GitHub's
// X-RateLimit-Reset / Retry-After pair on a 403 or 429.
package gitlabciruntime
