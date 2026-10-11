// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repositoryartifacts

import (
	"context"

	contentartifacts "github.com/eshu-hq/eshu/go/internal/content/artifacts"
)

const (
	// CandidateHydrationBudget is E, the per-request number of
	// config-artifact candidates the deployment-artifact load hydrates beyond
	// the per-repository floor (#7951). It is spent in RepoID order after the
	// anchor repository. The S6 census measured 7,774 candidates past the old
	// cap over the full related set of the slowest repository story on the QA
	// environment; the ruling requires E >= 1.5 x that extra (11,661), so E is
	// 12,000. Provisional until the deployed read confirms it.
	CandidateHydrationBudget = 12000

	// CandidatesBudgetReachedReason names a response whose config-artifact
	// rows are a lower bound because the request budget ran out: at least one
	// related repository has candidates inside its list window that were not
	// hydrated. Story limitations, context partial_reasons and service story
	// limitations carry it.
	CandidatesBudgetReachedReason = "deployment_artifacts_candidates_budget_reached"

	// RelatedFileListTruncatedReason names a response where at least one
	// related repository's file list returned a row past the 5,000-file
	// window: its candidates past the window are unknown.
	RelatedFileListTruncatedReason = "deployment_artifacts_related_file_list_truncated_at_5000"

	// AnchorFileListTruncatedReason names a service response whose own
	// repository file list (the service route's anchor list) returned a row
	// past the 5,000-file window: the deployment artifacts derived from that
	// list are a lower bound. The repository story and context routes disclose
	// their anchor list with their own reasons.
	AnchorFileListTruncatedReason = "deployment_artifacts_anchor_file_list_truncated_at_5000"
)

// configArtifactLoad is the per-repository config-artifact function the
// loader calls; production uses contentartifacts.LoadConfigPaths.
type configArtifactLoad func(
	ctx context.Context,
	reader contentartifacts.Reader,
	repo contentartifacts.Repository,
	files []contentartifacts.File,
	hasFileList bool,
	allowance int,
) ([]map[string]any, contentartifacts.ReadStats, error)

// configArtifactLoader holds the per-request candidate bounds of the
// deployment-artifact load: every repository hydrates floor candidates, and
// budget more candidates per request are spent in walk order. The fields are
// the test seam of the budget (not an environment setting): tests set a small
// budget to make it bite, or plant a load that ignores the allowance.
type configArtifactLoader struct {
	floor  int
	budget int
	load   configArtifactLoad
}

// productionConfigArtifactLoader is the loader every route uses.
var productionConfigArtifactLoader = configArtifactLoader{
	floor:  contentartifacts.HydrationFloor,
	budget: CandidateHydrationBudget,
	load:   contentartifacts.LoadConfigPaths,
}
