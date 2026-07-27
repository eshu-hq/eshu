// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cicdrun

// githubDeploymentsFixture is the fixture-shaped decode target for one
// bounded window of GitHub deployments plus their status events: the GitHub
// Deployments API (https://docs.github.com/en/rest/deployments/deployments)
// joined with the Deployment Statuses API
// (https://docs.github.com/en/rest/deployments/statuses), the same
// raw-map-to-JSON seam ghactionsruntime's source.go uses to hand RunSnapshot
// rows to GitHubActionsFixtureEnvelopes (see
// ghactionsruntime/source_deployments.go's deploymentFixtureRows).
type githubDeploymentsFixture struct {
	Deployments []githubDeploymentEntry `json:"deployments"`
}

// githubDeploymentEntry pairs one deployment with its bounded window of
// deployment_status events, mirroring how githubActionsFixture pairs one run
// with its jobs/artifacts.
type githubDeploymentEntry struct {
	Deployment githubDeployment         `json:"deployment"`
	Statuses   []githubDeploymentStatus `json:"statuses"`
}

// githubDeployment decodes the fields of a GitHub Deployment object
// (https://docs.github.com/en/rest/deployments/deployments) that
// deploymentEventEnvelope emits or needs for identity. Fields the collector
// never reads (url, node_id, payload, creator, statuses_url, repository_url,
// performed_via_github_app) are intentionally omitted -- repository identity
// instead derives from FixtureContext.SourceURI/Repository, since the
// Deployments API response carries no repository object (see
// deploymentRepositoryID).
type githubDeployment struct {
	ID                    any    `json:"id"`
	SHA                   string `json:"sha"`
	Ref                   string `json:"ref"`
	Task                  string `json:"task"`
	Environment           string `json:"environment"`
	OriginalEnvironment   string `json:"original_environment"`
	Description           string `json:"description"`
	CreatedAt             string `json:"created_at"`
	UpdatedAt             string `json:"updated_at"`
	TransientEnvironment  bool   `json:"transient_environment"`
	ProductionEnvironment bool   `json:"production_environment"`
}

// githubDeploymentStatus decodes the fields of a GitHub Deployment Status
// object (https://docs.github.com/en/rest/deployments/statuses) that
// deploymentEventEnvelope emits or needs for identity.
//
// The status carries its own Environment, and GitHub allows it to differ from
// the parent deployment's -- a redeploy can retarget, and the parent's
// original_environment field exists precisely to record that it moved. The
// status value is therefore the more specific truth for that transition and
// wins when present, with the parent's value as the fallback. Dropping it would
// publish the stale parent environment as deploy_event truth.
type githubDeploymentStatus struct {
	ID             any    `json:"id"`
	Environment    string `json:"environment"`
	State          string `json:"state"`
	Description    string `json:"description"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
	EnvironmentURL string `json:"environment_url"`
	LogURL         string `json:"log_url"`
}
