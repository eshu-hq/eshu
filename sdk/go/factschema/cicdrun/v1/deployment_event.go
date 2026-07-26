// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// DeploymentEvent is the schema-version-1 typed payload for the
// "ci.deployment_event" fact kind: one provider deployment or
// deployment-status event, matching the shape of GitHub's published
// Deployments API (the Deployment and Deployment Status objects,
// https://docs.github.com/en/rest/deployments/deployments and
// https://docs.github.com/en/rest/deployments/statuses). It is a raw
// provider observation, distinct from EnvironmentObservation in this
// package (which the ci_cd_run collector's GitHub Actions provider path
// derives from a run/job's environment, not from the Deployments API
// directly).
//
// The reducer's ci_cd_run_correlation domain decodes and consumes this kind:
// decodeCICDDeploymentEvent (go/internal/reducer/factschema_decode_cicdrun.go)
// decodes the envelope, attachDeploymentEventsToRuns
// (go/internal/reducer/ci_cd_run_correlation_deploy_events.go) joins it onto
// every run whose CommitSHA equals this struct's SHA — the deployment carries
// no run_id, which is exactly why the join is by sha — and
// classifyCICDDeploymentEventEnvironment
// (go/internal/reducer/ci_cd_run_correlation.go) selects the winning event
// per run and canonicalizes its Environment through environment.Canonical
// (for example "production" -> "prod") to produce the correlation's
// environment, stamped environment_evidence="deploy_event" on the
// run-correlation read model. The collector emitter is
// go/internal/collector/cicdrun/github_actions_deployments.go.
//
// Provider, DeploymentID, Environment, and SHA are required because GitHub's
// Deployments API always returns all four on every deployment object: the
// platform identity, the provider's own deployment id, the deployment's
// target environment (GitHub defaults an omitted environment to
// "production" server-side rather than ever returning one absent), and the
// resolved commit SHA (GitHub always resolves a caller-supplied branch/tag
// ref to a SHA before returning the object). A deployment event missing any
// of the four is not a valid observation of that API shape, so the decode
// seam rejects it as a classified input_invalid dead letter rather than
// silently collapsing an absent field to an empty string.
type DeploymentEvent struct {
	// Provider identifies the CI/CD or deployment platform that reported
	// this event (for example "github_actions"). Required — GitHub's Deployments
	// API response is always scoped to the platform that served it, and
	// this is the fact kind's own platform-identity segment, matching the
	// Provider convention every other kind in this package uses.
	Provider string `json:"provider"`

	// DeploymentID is the provider's own deployment identifier (GitHub's
	// numeric deployment id, stringified). Required — GitHub assigns one on
	// every deployment at creation time, and it is this fact kind's own
	// event identity: every deployment_status event this kind also models
	// carries the same parent DeploymentID.
	DeploymentID string `json:"deployment_id"`

	// Environment is the deployment's target environment (for example
	// "production" or "staging"). Required — GitHub's Deployments API
	// requires every deployment to declare a target environment, defaulting
	// an omitted value to "production" server-side, so an observed
	// deployment event always carries a non-absent value here.
	Environment string `json:"environment"`

	// SHA is the full commit SHA the deployment targets. Required — GitHub
	// always resolves and returns the deployed commit on the deployment
	// object, even when the caller originally created the deployment from a
	// branch or tag ref (see Ref for that original, unresolved value).
	SHA string `json:"sha"`

	// StatusID is the provider's own identifier for the specific
	// deployment_status event (GitHub's numeric status id, stringified),
	// present when this fact represents a status transition rather than the
	// deployment's own creation. Optional: a fact observed at
	// deployment-creation time has no status event yet.
	StatusID *string `json:"status_id,omitempty"`

	// Ref is the branch, tag, or SHA the caller originally supplied when
	// creating the deployment, before the provider resolved it to the SHA
	// field above. Optional: preserved as provenance of the caller's
	// original request, distinct from the resolved commit.
	Ref *string `json:"ref,omitempty"`

	// Task is the provider's deployment task name (for example "deploy" or
	// "deploy:migrations"), defaulting to "deploy" on GitHub when the
	// caller does not set one. Optional.
	Task *string `json:"task,omitempty"`

	// State is the provider's deployment status state. On GitHub's
	// deployment_status events this is one of: error, failure, inactive,
	// pending, success, queued, in_progress. Optional: absent when this
	// fact represents the deployment's creation rather than a status
	// transition, and modeled as a free string (not an enum) like every
	// other status/result field in this family — see Run.Result and
	// Step.Status for the same convention.
	State *string `json:"state,omitempty"`

	// OriginalEnvironment is the deployment's environment value at creation
	// time, which GitHub preserves on later status events even when a
	// deployment_status event reports a different Environment than its
	// parent deployment. Optional.
	OriginalEnvironment *string `json:"original_environment,omitempty"`

	// Description is the short human-readable description the provider or
	// the status event supplied. Optional.
	Description *string `json:"description,omitempty"`

	// CreatedAt is the event's creation timestamp as an RFC3339 string.
	// Optional.
	CreatedAt *string `json:"created_at,omitempty"`

	// UpdatedAt is the event's last-updated timestamp as an RFC3339 string.
	// Optional.
	UpdatedAt *string `json:"updated_at,omitempty"`

	// EnvironmentURL is the environment URL the provider exposes for a live
	// status event (for example a preview deployment's URL). Optional: only
	// deployment_status events carry it, and even then only when the caller
	// supplied one.
	EnvironmentURL *string `json:"environment_url,omitempty"`

	// LogURL is the provider's log URL for the deployment run associated
	// with this status event. Optional: only deployment_status events carry
	// it.
	LogURL *string `json:"log_url,omitempty"`

	// ProductionEnvironment reports whether the provider classifies
	// Environment as a production environment. Optional: GitHub infers this
	// from the environment name when the caller does not set it explicitly,
	// but the collector preserves whatever value the API returned rather
	// than re-deriving it.
	ProductionEnvironment *bool `json:"production_environment,omitempty"`

	// TransientEnvironment reports whether the provider classifies
	// Environment as short-lived (for example a per-pull-request preview
	// environment torn down after use). Optional, same provider-inference
	// caveat as ProductionEnvironment.
	TransientEnvironment *bool `json:"transient_environment,omitempty"`

	// RepositoryID is the canonical repository identifier
	// (repository:r_<hex>) the collector derives for the deployment's
	// owning repository, matching the join contract the git collector and
	// repositoryidentity.CanonicalRepositoryID enforce elsewhere in this
	// family (see Run.RepositoryID). Optional: modeled for future
	// correlation even though no reducer path reads it yet.
	RepositoryID *string `json:"repository_id,omitempty"`
}
