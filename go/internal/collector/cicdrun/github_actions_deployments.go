// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cicdrun

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	cicdrunv1 "github.com/eshu-hq/eshu/sdk/go/factschema/cicdrun/v1"
)

// deploymentProvider is the Provider segment ci.deployment_event facts carry.
// It is the SAME token every other fact in this family emits
// (ProviderGitHubActions), not a second vocabulary: the reducer buckets a run's
// evidence by provider, cassettes are compared against real collector output,
// and a consumer filtering ci.deployment_event by provider must not need to
// know that this one kind spelled it differently.
// It is deliberately "github", not ProviderGitHubActions ("github_actions"):
// the DeploymentEvent contract (sdk/go/factschema/cicdrun/v1/deployment_event.go)
// models a raw GitHub Deployments API observation, a platform-level surface
// any integration can create a deployment through, not an Actions-run-scoped
// one -- see that file's doc comment distinguishing it from
// EnvironmentObservation.
const deploymentProvider = string(ProviderGitHubActions)

// GitHubActionsDeploymentEnvelopes normalizes one fixture-shaped batch of
// GitHub deployments (each with its bounded window of deployment_status
// events) into ci.deployment_event facts, one per status row (or one with an
// empty status_id for a deployment with zero fetched statuses). Offline
// fixtures pass that payload directly; ghactionsruntime marshals its bounded
// DeploymentPage into the same shape before calling this normalizer (see
// ghactionsruntime/source_deployments.go's deploymentFixtureRows), mirroring
// how GitHubActionsFixtureEnvelopes consumes RunSnapshot.
func GitHubActionsDeploymentEnvelopes(raw []byte, ctx FixtureContext) ([]facts.Envelope, error) {
	if err := validateContext(ctx); err != nil {
		return nil, err
	}
	var fixture githubDeploymentsFixture
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&fixture); err != nil {
		return nil, fmt.Errorf("parse github deployments fixture: %w", err)
	}
	envelopes := make([]facts.Envelope, 0, len(fixture.Deployments))
	for index, entry := range fixture.Deployments {
		deploymentID, err := providerID(entry.Deployment.ID)
		if err != nil {
			return nil, fmt.Errorf("github deployment.id: %w", err)
		}
		if deploymentID == "" || trim(entry.Deployment.SHA) == "" || trim(entry.Deployment.Environment) == "" {
			warning, warningErr := GitHubActionsDeploymentWarningEnvelope(ctx, fmt.Sprintf("deployment:%d", index),
				"deployment_missing_required_field",
				"deployment metadata omitted provider id, sha, or environment")
			if warningErr != nil {
				return nil, warningErr
			}
			envelopes = append(envelopes, warning)
			continue
		}
		if len(entry.Statuses) == 0 {
			envelope, err := deploymentEventEnvelope(ctx, entry.Deployment, deploymentID, nil)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, envelope)
			continue
		}
		for _, status := range entry.Statuses {
			statusCopy := status
			envelope, err := deploymentEventEnvelope(ctx, entry.Deployment, deploymentID, &statusCopy)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, envelope)
		}
	}
	return deduplicateEnvelopes(envelopes), nil
}

// deploymentEventEnvelope builds one ci.deployment_event fact for a
// deployment (status == nil, a zero-status deployment) or for one of its
// deployment_status events (status != nil). It denormalizes the parent
// deployment's sha, ref, task, environment, original_environment, and the
// two boolean environment-classification flags onto the row so a status-row
// fact never depends on a separate deployment-row fact to be interpretable.
func deploymentEventEnvelope(ctx FixtureContext, deployment githubDeployment, deploymentID string, status *githubDeploymentStatus) (facts.Envelope, error) {
	statusID := ""
	if status != nil {
		id, err := providerID(status.ID)
		if err != nil {
			return facts.Envelope{}, fmt.Errorf("github deployment status.id: %w", err)
		}
		statusID = id
	}
	environment := trim(deployment.Environment)
	sha := trim(deployment.SHA)
	ref := trim(deployment.Ref)
	task := trim(deployment.Task)
	originalEnvironment := trim(deployment.OriginalEnvironment)
	description := trim(deployment.Description)
	createdAt := trim(deployment.CreatedAt)
	updatedAt := trim(deployment.UpdatedAt)
	state := ""
	environmentURL := ""
	logURL := ""
	if status != nil {
		state = trim(status.State)
		if trim(status.Description) != "" {
			description = trim(status.Description)
		}
		if trim(status.CreatedAt) != "" {
			createdAt = trim(status.CreatedAt)
		}
		if trim(status.UpdatedAt) != "" {
			updatedAt = trim(status.UpdatedAt)
		}
		environmentURL = trim(status.EnvironmentURL)
		logURL = trim(status.LogURL)
	}
	repositoryID := deploymentRepositoryID(ctx)
	payload := map[string]any{
		"collector_instance_id":  ctx.CollectorInstanceID,
		"provider":               deploymentProvider,
		"deployment_id":          deploymentID,
		"environment":            environment,
		"sha":                    sha,
		"status_id":              statusID,
		"ref":                    ref,
		"task":                   task,
		"state":                  state,
		"original_environment":   originalEnvironment,
		"description":            description,
		"created_at":             createdAt,
		"updated_at":             updatedAt,
		"environment_url":        environmentURL,
		"log_url":                logURL,
		"production_environment": deployment.ProductionEnvironment,
		"transient_environment":  deployment.TransientEnvironment,
		"repository_id":          repositoryID,
	}
	if err := mergeContractPayload(payload, func() (map[string]any, error) {
		return factschema.EncodeCICDDeploymentEvent(cicdrunv1.DeploymentEvent{
			Provider:              deploymentProvider,
			DeploymentID:          deploymentID,
			Environment:           environment,
			SHA:                   sha,
			StatusID:              optionalString(statusID),
			Ref:                   optionalString(ref),
			Task:                  optionalString(task),
			State:                 optionalString(state),
			OriginalEnvironment:   optionalString(originalEnvironment),
			Description:           optionalString(description),
			CreatedAt:             optionalString(createdAt),
			UpdatedAt:             optionalString(updatedAt),
			EnvironmentURL:        optionalString(environmentURL),
			LogURL:                optionalString(logURL),
			ProductionEnvironment: boolPtr(deployment.ProductionEnvironment),
			TransientEnvironment:  boolPtr(deployment.TransientEnvironment),
			RepositoryID:          optionalString(repositoryID),
		})
	}); err != nil {
		return facts.Envelope{}, err
	}
	// Keyed on the provider's immutable ids plus scope/repository -- never on
	// state or updated_at -- so re-polling the same status re-derives the
	// identical key and upserts, and pending -> in_progress -> success land
	// as three durable facts (three distinct status ids) rather than one
	// overwritten row. scope_id+repository are load-bearing: a GitLab
	// deployment iid is per-project, so provider+deployment_id alone would
	// collide across two different projects/scopes sharing a numeric id.
	stableKey := facts.StableID(facts.CICDDeploymentEventFactKind, map[string]any{
		"provider":      deploymentProvider,
		"scope_id":      ctx.ScopeID,
		"repository":    ctx.Repository,
		"deployment_id": deploymentID,
		"status_id":     statusID,
	})
	return newEnvelope(ctx, facts.CICDDeploymentEventFactKind, stableKey, deploymentID+":"+statusID, payload), nil
}

// deploymentRepositoryID derives the canonical repository_id
// (repository:r_<hex>) for a deployment event from FixtureContext.SourceURI,
// matching the join contract repositoryID (github_actions_helpers.go)
// enforces for run-scoped facts. It cannot reuse repositoryID's
// githubRepository-shaped input: GitHub's Deployments API response carries
// no repository object at all (only a "repository_url" API URL string,
// distinct from the html_url shape repositoryID expects), so the
// collector-supplied SourceURI (the target's canonical
// "https://github.com/<owner>/<repo>" per ghactionsruntime's
// validateTarget) is the reliable source here instead.
func deploymentRepositoryID(ctx FixtureContext) string {
	trimmed := trim(ctx.SourceURI)
	if trimmed == "" {
		return ""
	}
	id, err := repositoryidentity.CanonicalRepositoryID(trimmed, "")
	if err != nil {
		return ""
	}
	return id
}

// optionalString returns nil for a blank value and a pointer to value
// otherwise, matching the DeploymentEvent contract's *string "omitempty"
// optional fields (see sdk/go/factschema/cicdrun/v1/deployment_event.go).
func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return stringPtr(value)
}

// GitHubActionsDeploymentWarningEnvelope builds one ci.warning fact for a
// deployment-collection-scoped issue that is not about a specific ci.run --
// the whole deployments list being truncated, one deployment's required
// fields being missing, or one deployment event's sha matching no fetched
// run's head sha. Every other warning builder in this package
// (warningEnvelope in envelope.go, gitlabWarningEnvelope in
// gitlab_ci_fixture.go) keys off a run/pipeline and hardcodes that
// provider's name via sharedPayload; a deployment-scoped warning has no run
// to key off and belongs to the Deployments API surface (provider "github",
// not "github_actions"), so it gets its own minimal payload shape here
// instead of reusing sharedPayload.
func GitHubActionsDeploymentWarningEnvelope(ctx FixtureContext, warningKey, reason, message string) (facts.Envelope, error) {
	if err := validateContext(ctx); err != nil {
		return facts.Envelope{}, err
	}
	reason = trim(reason)
	payload := map[string]any{
		"collector_instance_id": ctx.CollectorInstanceID,
		"provider":              deploymentProvider,
		"reason":                reason,
		"message":               redactSensitiveText(trim(message)),
		"partial_generation":    true,
	}
	stableKey := facts.StableID(facts.CICDWarningFactKind, map[string]any{
		"key":      warningKey,
		"reason":   reason,
		"scope_id": ctx.ScopeID,
		"provider": deploymentProvider,
	})
	return newEnvelope(ctx, facts.CICDWarningFactKind, stableKey, warningKey, payload), nil
}
