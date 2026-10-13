// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

// Metric dimensions and bounded label values for the git collector's bounded
// command retry (#8091). They live in this sibling file so the frozen root
// contract.go does not grow, mirroring collector_stage.go.
const (
	// MetricDimensionHost labels git command retry metrics with the remote host
	// the collector builds its remote URLs for: github.com, bitbucket.org, or the
	// configured GitLab host (ESHU_GITLAB_HOST). The value comes from the same
	// provider mapping that builds the clone URL, never from a URL, path, or
	// token, so the set is the handful of configured providers plus "unknown".
	MetricDimensionHost = "host"
	// MetricDimensionScope labels eshu_dp_git_command_retry_budget_exhausted_total
	// with the budget that ran out. The set is closed: GitRetryScopeSweep.
	MetricDimensionScope = "scope"
)

// GitRetryScopeSweep is the scope label value of the per-sweep retry budget
// (ESHU_GIT_RETRY_SWEEP_BUDGET): the one cap shared by every repository in a
// sync cycle.
const GitRetryScopeSweep = "sweep"
