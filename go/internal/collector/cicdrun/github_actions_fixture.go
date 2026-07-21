// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cicdrun

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	cicdrunv1 "github.com/eshu-hq/eshu/sdk/go/factschema/cicdrun/v1"
)

// GitHubActionsFixtureEnvelopes normalizes one fixture-shaped GitHub Actions
// payload into reported-confidence CI/CD facts.
func GitHubActionsFixtureEnvelopes(raw []byte, ctx FixtureContext) ([]facts.Envelope, error) {
	if err := validateContext(ctx); err != nil {
		return nil, err
	}
	var fixture githubActionsFixture
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&fixture); err != nil {
		return nil, fmt.Errorf("parse github actions fixture: %w", err)
	}
	runID, err := providerID(fixture.Run.ID)
	if err != nil {
		return nil, fmt.Errorf("github actions fixture run.id: %w", err)
	}
	if runID == "" {
		return nil, fmt.Errorf("github actions fixture run.id must not be blank")
	}

	envelopes := make([]facts.Envelope, 0, 2+len(fixture.Jobs)+len(fixture.Artifacts)+len(fixture.Triggers))
	workflowID, err := providerID(fixture.Workflow.ID)
	if err != nil {
		return nil, fmt.Errorf("github actions fixture workflow.id: %w", err)
	}
	if workflowID != "" || fixture.Workflow.Path != "" || fixture.Workflow.Name != "" {
		envelope, err := pipelineDefinitionEnvelope(ctx, fixture)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	run, err := runEnvelope(ctx, fixture.Run)
	if err != nil {
		return nil, err
	}
	envelopes = append(envelopes, run)
	if repositoryID(fixture.Run.Repository, ctx) == "" || trim(fixture.Run.HeadSHA) == "" {
		warning, err := warningEnvelope(ctx, fixture.Run, "run:anchors", "run_missing_repository_or_commit", "run metadata omitted repository locator or commit SHA")
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, warning)
	}
	for jobIndex, job := range fixture.Jobs {
		jobID, err := providerID(job.ID)
		if err != nil || jobID == "" {
			warning, warningErr := warningEnvelope(ctx, fixture.Run, fmt.Sprintf("job:%d", jobIndex), "job_missing_id", "job metadata omitted provider job ID")
			if warningErr != nil {
				return nil, warningErr
			}
			envelopes = append(envelopes, warning)
			continue
		}
		jobFact, err := jobEnvelope(ctx, fixture.Run, job)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, jobFact)
		for _, step := range job.Steps {
			stepNumber, err := providerID(step.Number)
			if err != nil || stepNumber == "" {
				warning, warningErr := warningEnvelope(ctx, fixture.Run, "step:"+jobID+":"+trim(step.Name), "step_missing_number", "step metadata omitted provider step number")
				if warningErr != nil {
					return nil, warningErr
				}
				envelopes = append(envelopes, warning)
				continue
			}
			stepFact, err := stepEnvelope(ctx, fixture.Run, job, step)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, stepFact)
		}
		if trim(job.Environment) != "" {
			envFact, err := environmentEnvelope(ctx, fixture.Run, job)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, envFact)
		}
	}
	for artifactIndex, artifact := range fixture.Artifacts {
		artifactID, err := providerID(artifact.ID)
		if err != nil || artifactID == "" {
			warning, warningErr := warningEnvelope(ctx, fixture.Run, fmt.Sprintf("artifact:%d", artifactIndex), "artifact_missing_id", "artifact metadata omitted provider artifact ID")
			if warningErr != nil {
				return nil, warningErr
			}
			envelopes = append(envelopes, warning)
			continue
		}
		if !artifactMatchesRun(fixture.Run, artifact) {
			warning, warningErr := warningEnvelope(ctx, fixture.Run, "artifact:"+artifactID, "artifact_run_mismatch", "artifact workflow_run reference did not match enclosing run")
			if warningErr != nil {
				return nil, warningErr
			}
			envelopes = append(envelopes, warning)
			continue
		}
		artifactFact, err := artifactEnvelope(ctx, fixture.Run, artifact)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, artifactFact)
		if trim(artifact.Digest) == "" {
			warning, err := warningEnvelope(ctx, fixture.Run, "artifact:"+artifactID, "artifact_missing_digest", "artifact metadata did not include a digest")
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, warning)
		}
	}
	for triggerIndex, trigger := range fixture.Triggers {
		sourceRunID, err := providerID(trigger.SourceRunID)
		if err != nil || sourceRunID == "" || trim(trigger.SourceProvider) == "" || trim(trigger.TriggerKind) == "" {
			warning, warningErr := warningEnvelope(ctx, fixture.Run, fmt.Sprintf("trigger:%d", triggerIndex), "trigger_edge_missing_anchor", "trigger edge omitted source provider, source run ID, or trigger kind")
			if warningErr != nil {
				return nil, warningErr
			}
			envelopes = append(envelopes, warning)
			continue
		}
		triggerFact, err := triggerEnvelope(ctx, fixture.Run, trigger)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, triggerFact)
	}
	for warningIndex, warning := range fixture.Warnings {
		warningFact, err := warningEnvelope(ctx, fixture.Run, fmt.Sprintf("fixture-warning:%d", warningIndex), warning.Reason, warning.Message)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, warningFact)
	}
	if fixture.JobsPartial {
		warning, err := warningEnvelope(ctx, fixture.Run, "jobs:partial", "partial_jobs_payload", "job metadata was partial or unavailable")
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, warning)
	}
	if fixture.ArtifactsPartial {
		warning, err := warningEnvelope(ctx, fixture.Run, "artifacts:partial", "partial_artifacts_payload", "artifact metadata was partial or unavailable")
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, warning)
	}
	return deduplicateEnvelopes(envelopes), nil
}

func pipelineDefinitionEnvelope(ctx FixtureContext, fixture githubActionsFixture) (facts.Envelope, error) {
	run := fixture.Run
	workflow := fixture.Workflow
	workflowID, err := providerID(workflow.ID)
	if err != nil {
		return facts.Envelope{}, err
	}
	payload, err := sharedPayload(ctx, run)
	if err != nil {
		return facts.Envelope{}, err
	}
	payload["workflow_id"] = workflowID
	payload["workflow_name"] = trim(workflow.Name)
	payload["workflow_path"] = trim(workflow.Path)
	payload["workflow_state"] = trim(workflow.State)
	payload["trigger"] = trim(workflow.Trigger)
	payload["repository_id"] = repositoryID(run.Repository, ctx)
	payload["provider_repository_id"] = providerRepositoryID(run.Repository, ctx)
	payload["definition_kind"] = "workflow"
	stableKey := facts.StableID(facts.CICDPipelineDefinitionFactKind, map[string]any{
		"provider":      ProviderGitHubActions,
		"repository_id": payload["repository_id"],
		"workflow_id":   workflowID,
		"workflow_path": trim(workflow.Path),
	})
	return newEnvelope(ctx, facts.CICDPipelineDefinitionFactKind, stableKey, workflowID, payload), nil
}

func runEnvelope(ctx FixtureContext, run githubRun) (facts.Envelope, error) {
	runID, err := providerID(run.ID)
	if err != nil {
		return facts.Envelope{}, err
	}
	runNumber, err := providerID(run.RunNumber)
	if err != nil {
		return facts.Envelope{}, err
	}
	payload, err := sharedPayload(ctx, run)
	if err != nil {
		return facts.Envelope{}, err
	}
	payload["run_number"] = runNumber
	payload["workflow_name"] = trim(run.Name)
	payload["event"] = trim(run.Event)
	payload["status"] = trim(run.Status)
	payload["result"] = trim(run.Conclusion)
	payload["branch"] = trim(run.HeadBranch)
	payload["commit_sha"] = trim(run.HeadSHA)
	canonicalRepoID := repositoryID(run.Repository, ctx)
	providerRepoID := providerRepositoryID(run.Repository, ctx)
	payload["repository_id"] = canonicalRepoID
	payload["provider_repository_id"] = providerRepoID
	payload["repository_url"] = trim(run.Repository.HTMLURL)
	payload["actor"] = trim(run.Actor.Login)
	payload["started_at"] = trim(run.RunStartedAt)
	payload["updated_at"] = trim(run.UpdatedAt)
	payload["url"] = stripSensitiveURL(run.HTMLURL)
	payload["correlation_anchors"] = nonEmptyStrings(canonicalRepoID, trim(run.HeadSHA), runID)
	if err := mergeContractPayload(payload, func() (map[string]any, error) {
		return factschema.EncodeCICDRun(cicdrunv1.Run{
			Provider:             string(ProviderGitHubActions),
			RunID:                runID,
			RunAttempt:           stringPtr(payload["run_attempt"].(string)),
			RunNumber:            stringPtr(runNumber),
			WorkflowName:         stringPtr(trim(run.Name)),
			Event:                stringPtr(trim(run.Event)),
			Status:               stringPtr(trim(run.Status)),
			Result:               stringPtr(trim(run.Conclusion)),
			Branch:               stringPtr(trim(run.HeadBranch)),
			CommitSHA:            stringPtr(trim(run.HeadSHA)),
			RepositoryID:         stringPtr(canonicalRepoID),
			ProviderRepositoryID: stringPtr(providerRepoID),
			RepositoryURL:        stringPtr(trim(run.Repository.HTMLURL)),
			Actor:                stringPtr(trim(run.Actor.Login)),
			StartedAt:            stringPtr(trim(run.RunStartedAt)),
			UpdatedAt:            stringPtr(trim(run.UpdatedAt)),
			URL:                  stringPtr(stripSensitiveURL(run.HTMLURL)),
			CorrelationAnchors:   nonEmptyStrings(canonicalRepoID, trim(run.HeadSHA), runID),
			CollectorInstanceID:  stringPtr(ctx.CollectorInstanceID),
		})
	}); err != nil {
		return facts.Envelope{}, err
	}
	stableKey := facts.StableID(facts.CICDRunFactKind, map[string]any{
		"provider":    ProviderGitHubActions,
		"run_attempt": payload["run_attempt"],
		"run_id":      runID,
	})
	return newEnvelope(ctx, facts.CICDRunFactKind, stableKey, runID, payload), nil
}

func jobEnvelope(ctx FixtureContext, run githubRun, job githubJob) (facts.Envelope, error) {
	jobID, err := providerID(job.ID)
	if err != nil {
		return facts.Envelope{}, err
	}
	payload, err := sharedPayload(ctx, run)
	if err != nil {
		return facts.Envelope{}, err
	}
	payload["job_id"] = jobID
	payload["job_name"] = trim(job.Name)
	payload["status"] = trim(job.Status)
	payload["result"] = trim(job.Conclusion)
	payload["started_at"] = trim(job.StartedAt)
	payload["completed_at"] = trim(job.CompletedAt)
	payload["runner_labels"] = append([]string(nil), job.Labels...)
	stableKey := facts.StableID(facts.CICDJobFactKind, map[string]any{
		"job_id":      jobID,
		"run_attempt": payload["run_attempt"],
		"run_id":      payload["run_id"],
	})
	return newEnvelope(ctx, facts.CICDJobFactKind, stableKey, jobID, payload), nil
}

func stepEnvelope(ctx FixtureContext, run githubRun, job githubJob, step githubStep) (facts.Envelope, error) {
	stepNumber, err := providerID(step.Number)
	if err != nil {
		return facts.Envelope{}, err
	}
	jobID, err := providerID(job.ID)
	if err != nil {
		return facts.Envelope{}, err
	}
	payload, err := sharedPayload(ctx, run)
	if err != nil {
		return facts.Envelope{}, err
	}
	payload["job_id"] = jobID
	payload["step_number"] = stepNumber
	payload["step_name"] = trim(step.Name)
	payload["status"] = trim(step.Status)
	payload["result"] = trim(step.Conclusion)
	payload["started_at"] = trim(step.StartedAt)
	payload["completed_at"] = trim(step.CompletedAt)
	payload["action_ref"] = actionReference(step.Name)
	if err := mergeContractPayload(payload, func() (map[string]any, error) {
		return factschema.EncodeCICDStep(cicdrunv1.Step{
			Provider:            string(ProviderGitHubActions),
			RunID:               payload["run_id"].(string),
			RunAttempt:          stringPtr(payload["run_attempt"].(string)),
			JobID:               stringPtr(jobID),
			StepNumber:          stringPtr(stepNumber),
			StepName:            stringPtr(trim(step.Name)),
			Status:              stringPtr(trim(step.Status)),
			Result:              stringPtr(trim(step.Conclusion)),
			StartedAt:           stringPtr(trim(step.StartedAt)),
			CompletedAt:         stringPtr(trim(step.CompletedAt)),
			ActionRef:           stringPtr(actionReference(step.Name)),
			CollectorInstanceID: stringPtr(ctx.CollectorInstanceID),
		})
	}); err != nil {
		return facts.Envelope{}, err
	}
	stableKey := facts.StableID(facts.CICDStepFactKind, map[string]any{
		"job_id":      jobID,
		"run_attempt": payload["run_attempt"],
		"run_id":      payload["run_id"],
		"step_number": stepNumber,
	})
	return newEnvelope(ctx, facts.CICDStepFactKind, stableKey, jobID+":"+stepNumber, payload), nil
}

func artifactEnvelope(ctx FixtureContext, run githubRun, artifact githubArtifact) (facts.Envelope, error) {
	artifactID, err := providerID(artifact.ID)
	if err != nil {
		return facts.Envelope{}, err
	}
	payload, err := sharedPayload(ctx, run)
	if err != nil {
		return facts.Envelope{}, err
	}
	payload["artifact_id"] = artifactID
	payload["artifact_name"] = trim(artifact.Name)
	payload["artifact_type"] = defaultArtifactType(artifact)
	payload["artifact_digest"] = trim(artifact.Digest)
	payload["size_bytes"] = artifact.SizeBytes
	payload["expired"] = artifact.Expired
	payload["created_at"] = trim(artifact.CreatedAt)
	payload["expires_at"] = trim(artifact.ExpiresAt)
	payload["download_url"] = stripSensitiveURL(artifact.ArchiveDownloadURL)
	payload["correlation_anchors"] = nonEmptyStrings(payload["run_id"].(string), trim(artifact.Digest))
	if err := mergeContractPayload(payload, func() (map[string]any, error) {
		return factschema.EncodeCICDArtifact(cicdrunv1.Artifact{
			Provider:            string(ProviderGitHubActions),
			RunID:               payload["run_id"].(string),
			RunAttempt:          stringPtr(payload["run_attempt"].(string)),
			ArtifactID:          stringPtr(artifactID),
			ArtifactName:        stringPtr(trim(artifact.Name)),
			ArtifactType:        stringPtr(defaultArtifactType(artifact)),
			ArtifactDigest:      stringPtr(trim(artifact.Digest)),
			SizeBytes:           int64Ptr(artifact.SizeBytes),
			Expired:             boolPtr(artifact.Expired),
			CreatedAt:           stringPtr(trim(artifact.CreatedAt)),
			ExpiresAt:           stringPtr(trim(artifact.ExpiresAt)),
			DownloadURL:         stringPtr(stripSensitiveURL(artifact.ArchiveDownloadURL)),
			CorrelationAnchors:  nonEmptyStrings(payload["run_id"].(string), trim(artifact.Digest)),
			CollectorInstanceID: stringPtr(ctx.CollectorInstanceID),
		})
	}); err != nil {
		return facts.Envelope{}, err
	}
	stableKey := facts.StableID(facts.CICDArtifactFactKind, map[string]any{
		"artifact_id": artifactID,
		"run_attempt": payload["run_attempt"],
		"run_id":      payload["run_id"],
	})
	return newEnvelope(ctx, facts.CICDArtifactFactKind, stableKey, artifactID, payload), nil
}

func environmentEnvelope(ctx FixtureContext, run githubRun, job githubJob) (facts.Envelope, error) {
	jobID, err := providerID(job.ID)
	if err != nil {
		return facts.Envelope{}, err
	}
	payload, err := sharedPayload(ctx, run)
	if err != nil {
		return facts.Envelope{}, err
	}
	payload["job_id"] = jobID
	payload["environment"] = trim(job.Environment)
	payload["deployment_status"] = trim(job.DeploymentStatus)
	if err := mergeContractPayload(payload, func() (map[string]any, error) {
		return factschema.EncodeCICDEnvironmentObservation(cicdrunv1.EnvironmentObservation{
			Provider:            string(ProviderGitHubActions),
			RunID:               payload["run_id"].(string),
			RunAttempt:          stringPtr(payload["run_attempt"].(string)),
			JobID:               stringPtr(jobID),
			Environment:         stringPtr(trim(job.Environment)),
			DeploymentStatus:    stringPtr(trim(job.DeploymentStatus)),
			CollectorInstanceID: stringPtr(ctx.CollectorInstanceID),
		})
	}); err != nil {
		return facts.Envelope{}, err
	}
	stableKey := facts.StableID(facts.CICDEnvironmentObservationFactKind, map[string]any{
		"environment": trim(job.Environment),
		"job_id":      jobID,
		"run_attempt": payload["run_attempt"],
		"run_id":      payload["run_id"],
	})
	return newEnvelope(ctx, facts.CICDEnvironmentObservationFactKind, stableKey, jobID+":"+trim(job.Environment), payload), nil
}

func triggerEnvelope(ctx FixtureContext, run githubRun, trigger githubTrigger) (facts.Envelope, error) {
	sourceRunID, err := providerID(trigger.SourceRunID)
	if err != nil {
		return facts.Envelope{}, err
	}
	payload, err := sharedPayload(ctx, run)
	if err != nil {
		return facts.Envelope{}, err
	}
	payload["trigger_kind"] = trim(trigger.TriggerKind)
	payload["source_provider"] = trim(trigger.SourceProvider)
	payload["source_run_id"] = sourceRunID
	if err := mergeContractPayload(payload, func() (map[string]any, error) {
		return factschema.EncodeCICDTriggerEdge(cicdrunv1.TriggerEdge{
			Provider:            string(ProviderGitHubActions),
			RunID:               payload["run_id"].(string),
			RunAttempt:          stringPtr(payload["run_attempt"].(string)),
			TriggerKind:         stringPtr(trim(trigger.TriggerKind)),
			SourceProvider:      stringPtr(trim(trigger.SourceProvider)),
			SourceRunID:         stringPtr(sourceRunID),
			CollectorInstanceID: stringPtr(ctx.CollectorInstanceID),
		})
	}); err != nil {
		return facts.Envelope{}, err
	}
	stableKey := facts.StableID(facts.CICDTriggerEdgeFactKind, map[string]any{
		"run_attempt":     payload["run_attempt"],
		"run_id":          payload["run_id"],
		"source_provider": trim(trigger.SourceProvider),
		"source_run_id":   sourceRunID,
		"trigger_kind":    trim(trigger.TriggerKind),
	})
	return newEnvelope(ctx, facts.CICDTriggerEdgeFactKind, stableKey, sourceRunID, payload), nil
}

func warningEnvelope(ctx FixtureContext, run githubRun, warningKey, reason, message string) (facts.Envelope, error) {
	reason = trim(reason)
	payload, err := sharedPayload(ctx, run)
	if err != nil {
		return facts.Envelope{}, err
	}
	payload["reason"] = reason
	payload["message"] = redactSensitiveText(trim(message))
	payload["partial_generation"] = true
	stableKey := facts.StableID(facts.CICDWarningFactKind, map[string]any{
		"key":         warningKey,
		"reason":      reason,
		"run_attempt": payload["run_attempt"],
		"run_id":      payload["run_id"],
	})
	return newEnvelope(ctx, facts.CICDWarningFactKind, stableKey, warningKey, payload), nil
}
