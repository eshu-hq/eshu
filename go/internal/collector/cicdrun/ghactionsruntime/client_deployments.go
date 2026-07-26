// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ghactionsruntime

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

// DeploymentSnapshot carries one raw provider deployment plus its bounded
// window of deployment_status events, mirroring RunSnapshot's raw-map shape
// so the cicdrun normalizer decodes both through the same JSON-marshal seam
// (see source_deployments.go's deploymentFixtureRows).
type DeploymentSnapshot struct {
	Deployment      map[string]any
	Statuses        []map[string]any
	StatusesPartial bool
}

// DeploymentPage carries the bounded window of deployments one claim
// fetched, plus whether the provider's deployments listing indicated
// additional deployments exist beyond the window. GitHub's deployments-list
// endpoint (unlike actions/runs, jobs, and artifacts) returns a bare JSON
// array with no total_count wrapper, so Truncated is always the full-page
// heuristic (fetched length == MaxDeployments) -- see runsPageTruncated's
// own fallback branch for the identical heuristic on runs.
type DeploymentPage struct {
	Snapshots []DeploymentSnapshot
	Truncated bool
}

// FetchDeployments returns the configured repository's most recent
// deployments (bounded by target.MaxDeployments) plus each deployment's
// bounded deployment_status window, mirroring FetchRuns' run+jobs+artifacts
// shape. A deployment carries no run_id of its own -- attaching it to a
// workflow run is the reducer's job
// (ci_cd_run_correlation_deploy_events.go, joining by sha), not this
// fetch's.
func (c GitHubClient) FetchDeployments(ctx context.Context, target TargetConfig) (DeploymentPage, error) {
	target, err := validateTarget(target)
	if err != nil {
		return DeploymentPage{}, err
	}
	deployments, truncated, err := c.fetchDeployments(ctx, target)
	if err != nil {
		return DeploymentPage{}, err
	}
	snapshots := make([]DeploymentSnapshot, 0, len(deployments))
	for _, deployment := range deployments {
		deploymentID, err := numericProviderID(deployment["id"])
		if err != nil {
			return DeploymentPage{}, fmt.Errorf("github deployment.id: %w", err)
		}
		statuses, statusesTruncated, err := c.fetchDeploymentStatuses(ctx, target, deploymentID)
		if err != nil {
			return DeploymentPage{}, err
		}
		snapshots = append(snapshots, DeploymentSnapshot{
			Deployment:      deployment,
			Statuses:        statuses,
			StatusesPartial: statusesTruncated,
		})
	}
	return DeploymentPage{
		Snapshots: snapshots,
		Truncated: truncated,
	}, nil
}

// fetchDeployments issues one bounded GET against
// /repos/{owner}/{repo}/deployments, GitHub's deployments-list endpoint.
// Unlike fetchRunPage/fetchJobs/fetchArtifacts, the response is a bare JSON
// array with no total_count wrapper, so truncation is reported through the
// full-page heuristic (fetched length == the requested per_page bound)
// rather than an exact provider-reported total.
func (c GitHubClient) fetchDeployments(ctx context.Context, target TargetConfig) ([]map[string]any, bool, error) {
	path := fmt.Sprintf("/repos/%s/deployments", target.Repository)
	endpoint, err := targetURL(target, path, map[string]string{
		"per_page": strconv.Itoa(target.MaxDeployments),
	})
	if err != nil {
		return nil, false, err
	}
	var deployments []map[string]any
	if err := c.getJSON(ctx, target, endpoint, &deployments); err != nil {
		return nil, false, fmt.Errorf("fetch github deployments: %w", err)
	}
	return deployments, len(deployments) == target.MaxDeployments, nil
}

// fetchDeploymentStatuses issues one bounded GET against
// /repos/{owner}/{repo}/deployments/{deployment_id}/statuses, GitHub's
// deployment-statuses endpoint. Like fetchDeployments, the response is a
// bare JSON array with no total_count wrapper.
func (c GitHubClient) fetchDeploymentStatuses(ctx context.Context, target TargetConfig, deploymentID string) ([]map[string]any, bool, error) {
	path := fmt.Sprintf("/repos/%s/deployments/%s/statuses", target.Repository, url.PathEscape(deploymentID))
	endpoint, err := targetURL(target, path, map[string]string{
		"per_page": strconv.Itoa(target.MaxDeployments),
	})
	if err != nil {
		return nil, false, err
	}
	var statuses []map[string]any
	if err := c.getJSON(ctx, target, endpoint, &statuses); err != nil {
		return nil, false, fmt.Errorf("fetch github deployment statuses: %w", err)
	}
	return statuses, len(statuses) == target.MaxDeployments, nil
}
