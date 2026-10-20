// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repositoryartifacts

import (
	"context"

	contentartifacts "github.com/eshu-hq/eshu/go/internal/content/artifacts"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

const (
	// CandidateHydrationBudget is E, the per-request number of
	// config-artifact candidates the deployment-artifact load hydrates beyond
	// the per-repository floor (#7951). It is spent in RepoID order after the
	// anchor repository. The S6 census measured 7,774 candidates past the old
	// cap over the full related set of the slowest repository story on the QA
	// environment; the ruling requires E >= 1.5 x that extra (11,661), so E is
	// 12,000. The anchor repository draws from E first, and the 1.5x sizing
	// covers the related set only, so an anchor with many candidates leaves
	// less for the related set; the deployed read measures the whole-request
	// draw. Confirmed for R1 by the deployed read 2026-10-19 on QA image
	// sha-aefdc43b28: 15,997 whole-request draw with zero unhydrated and no
	// budget-capped repository. The 1.5x census sizing is not re-proven
	// (related set drifts; extra_used unobservable); any future bite
	// self-discloses via CandidatesBudgetReachedReason.
	CandidateHydrationBudget = 12000

	// CandidatesBudgetReachedReason names a response whose config-artifact
	// rows are a lower bound because the request budget ran out: at least one
	// related repository has candidates inside its list window that were not
	// hydrated. Story limitations, context partial_reasons and service story
	// limitations carry it.
	CandidatesBudgetReachedReason = querycontract.DeploymentArtifactsCandidatesBudgetReachedReason

	// RelatedFileListTruncatedReason names a response where at least one
	// related repository's file list returned a row past the 5,000-file
	// window: its candidates past the window are unknown.
	RelatedFileListTruncatedReason = querycontract.DeploymentArtifactsRelatedFileListTruncatedReason

	// AnchorFileListTruncatedReason names a service response whose own
	// repository file list (the service route's anchor list) returned a row
	// past the 5,000-file window: the deployment artifacts derived from that
	// list are a lower bound. The repository story and context routes disclose
	// their anchor list with their own reasons.
	AnchorFileListTruncatedReason = querycontract.DeploymentArtifactsAnchorFileListTruncatedReason
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
	// shadow runs the stored-vs-live compare after the live load (#8155
	// PR3c). The zero value disables it: no stored statement, no report.
	shadow ShadowConfig
}

// productionConfigArtifactLoader is the loader every route uses.
var productionConfigArtifactLoader = configArtifactLoader{
	floor:  contentartifacts.HydrationFloor,
	budget: CandidateHydrationBudget,
	load:   contentartifacts.LoadConfigPaths,
}
