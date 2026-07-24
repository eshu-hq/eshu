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
package cicdrun
