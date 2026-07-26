// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package cicdrun normalizes GitHub Actions and GitLab CI/CD provider
// evidence into durable facts for the ci_cd_run collector family.
//
// The parent package owns the schema-preserving normalizers used by offline
// fixtures and by the hosted ghactionsruntime subpackage. Both providers emit
// the SAME ci.* fact kinds and the SAME reducer join-key shape (provider,
// run_id, run_attempt) so GitHub Actions and GitLab CI are two providers on
// one contract, not parallel fact-kind families (issue #5427). GitLab CI
// scope is narrower than GitHub Actions today: no ci.pipeline_definition (no
// separate workflow resource distinct from the pipeline), no ci.step (no
// step-level breakdown), no ci.trigger_edge / ci.environment_observation
// (out of v1 scope, matching GitHub Actions' own live client). It produces
// reported-confidence facts for pipeline definitions, runs, jobs, steps,
// artifacts, trigger edges, environment observations, and warnings. Hosted
// API polling, credentials, request budgets, claim resolution, runtime
// telemetry, and status belong in ghactionsruntime; graph writes and
// deployment truth stay reducer-owned.
//
// GitHubActionsDeploymentEnvelopes (github_actions_deployments.go) is a
// second, run-independent normalizer in this same package: it turns GitHub
// Deployments API observations into ci.deployment_event facts (#5425 STEP
// 3), one per deployment_status event. A deployment carries no run_id, so
// the reducer attaches it to a run by matching commit sha instead of a join
// key this package can build at emission time.
package cicdrun
