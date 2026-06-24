// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package cicdrun normalizes GitHub Actions CI/CD provider evidence into
// durable facts for the ci_cd_run collector family.
//
// The parent package owns the schema-preserving normalizer used by offline
// fixtures and by the hosted ghactionsruntime subpackage. It produces
// reported-confidence facts for pipeline definitions, runs, jobs, steps,
// artifacts, trigger edges, environment observations, and warnings. Hosted API
// polling, credentials, request budgets, claim resolution, runtime telemetry,
// and status belong in ghactionsruntime; graph writes and deployment truth stay
// reducer-owned.
package cicdrun
