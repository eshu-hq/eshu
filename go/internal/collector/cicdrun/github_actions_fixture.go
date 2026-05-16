package cicdrun

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// GitHubActionsFixtureEnvelopes normalizes one offline GitHub Actions fixture
// into reported-confidence CI/CD facts.
func GitHubActionsFixtureEnvelopes(raw []byte, ctx FixtureContext) ([]facts.Envelope, error) {
	if err := validateContext(ctx); err != nil {
		return nil, err
	}
	var fixture githubActionsFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		return nil, fmt.Errorf("parse github actions fixture: %w", err)
	}
	runID := providerID(fixture.Run.ID)
	if runID == "" {
		return nil, fmt.Errorf("github actions fixture run.id must not be blank")
	}

	envelopes := make([]facts.Envelope, 0, 2+len(fixture.Jobs)+len(fixture.Artifacts)+len(fixture.Triggers))
	if fixture.Workflow.Path != "" || fixture.Workflow.Name != "" {
		envelopes = append(envelopes, pipelineDefinitionEnvelope(ctx, fixture))
	}
	envelopes = append(envelopes, runEnvelope(ctx, fixture.Run))
	for _, job := range fixture.Jobs {
		envelopes = append(envelopes, jobEnvelope(ctx, fixture.Run, job))
		for _, step := range job.Steps {
			envelopes = append(envelopes, stepEnvelope(ctx, fixture.Run, job, step))
		}
		if trim(job.Environment) != "" {
			envelopes = append(envelopes, environmentEnvelope(ctx, fixture.Run, job))
		}
	}
	for _, artifact := range fixture.Artifacts {
		envelopes = append(envelopes, artifactEnvelope(ctx, fixture.Run, artifact))
		if trim(artifact.Digest) == "" {
			envelopes = append(envelopes, warningEnvelope(ctx, fixture.Run, "artifact_missing_digest", "artifact metadata did not include a digest"))
		}
	}
	for _, trigger := range fixture.Triggers {
		envelopes = append(envelopes, triggerEnvelope(ctx, fixture.Run, trigger))
	}
	for _, warning := range fixture.Warnings {
		envelopes = append(envelopes, warningEnvelope(ctx, fixture.Run, warning.Reason, warning.Message))
	}
	if fixture.JobsPartial {
		envelopes = append(envelopes, warningEnvelope(ctx, fixture.Run, "partial_jobs_payload", "job metadata was partial or unavailable"))
	}
	return envelopes, nil
}

func pipelineDefinitionEnvelope(ctx FixtureContext, fixture githubActionsFixture) facts.Envelope {
	run := fixture.Run
	workflow := fixture.Workflow
	workflowID := providerID(workflow.ID)
	payload := sharedPayload(ctx, run)
	payload["workflow_id"] = workflowID
	payload["workflow_name"] = trim(workflow.Name)
	payload["workflow_path"] = trim(workflow.Path)
	payload["workflow_state"] = trim(workflow.State)
	payload["trigger"] = trim(workflow.Trigger)
	payload["repository_id"] = repositoryID(run.Repository)
	payload["definition_kind"] = "workflow"
	stableKey := facts.StableID(facts.CICDPipelineDefinitionFactKind, map[string]any{
		"provider":      ProviderGitHubActions,
		"repository_id": payload["repository_id"],
		"workflow_id":   workflowID,
		"workflow_path": trim(workflow.Path),
	})
	return newEnvelope(ctx, facts.CICDPipelineDefinitionFactKind, stableKey, workflowID, payload)
}

func runEnvelope(ctx FixtureContext, run githubRun) facts.Envelope {
	runID := providerID(run.ID)
	payload := sharedPayload(ctx, run)
	payload["run_number"] = providerID(run.RunNumber)
	payload["workflow_name"] = trim(run.Name)
	payload["event"] = trim(run.Event)
	payload["status"] = trim(run.Status)
	payload["result"] = trim(run.Conclusion)
	payload["branch"] = trim(run.HeadBranch)
	payload["commit_sha"] = trim(run.HeadSHA)
	payload["repository_id"] = repositoryID(run.Repository)
	payload["repository_url"] = trim(run.Repository.HTMLURL)
	payload["actor"] = trim(run.Actor.Login)
	payload["started_at"] = trim(run.RunStartedAt)
	payload["updated_at"] = trim(run.UpdatedAt)
	payload["url"] = stripSensitiveURL(run.HTMLURL)
	payload["correlation_anchors"] = nonEmptyStrings(repositoryID(run.Repository), trim(run.HeadSHA), runID)
	stableKey := facts.StableID(facts.CICDRunFactKind, map[string]any{
		"provider":    ProviderGitHubActions,
		"run_attempt": defaultAttempt(providerID(run.RunAttempt)),
		"run_id":      runID,
	})
	return newEnvelope(ctx, facts.CICDRunFactKind, stableKey, runID, payload)
}

func jobEnvelope(ctx FixtureContext, run githubRun, job githubJob) facts.Envelope {
	jobID := providerID(job.ID)
	payload := sharedPayload(ctx, run)
	payload["job_id"] = jobID
	payload["job_name"] = trim(job.Name)
	payload["status"] = trim(job.Status)
	payload["result"] = trim(job.Conclusion)
	payload["started_at"] = trim(job.StartedAt)
	payload["completed_at"] = trim(job.CompletedAt)
	payload["runner_labels"] = append([]string(nil), job.Labels...)
	stableKey := facts.StableID(facts.CICDJobFactKind, map[string]any{
		"job_id":      jobID,
		"run_attempt": defaultAttempt(providerID(run.RunAttempt)),
		"run_id":      providerID(run.ID),
	})
	return newEnvelope(ctx, facts.CICDJobFactKind, stableKey, jobID, payload)
}

func stepEnvelope(ctx FixtureContext, run githubRun, job githubJob, step githubStep) facts.Envelope {
	stepNumber := providerID(step.Number)
	payload := sharedPayload(ctx, run)
	payload["job_id"] = providerID(job.ID)
	payload["step_number"] = stepNumber
	payload["step_name"] = trim(step.Name)
	payload["status"] = trim(step.Status)
	payload["result"] = trim(step.Conclusion)
	payload["started_at"] = trim(step.StartedAt)
	payload["completed_at"] = trim(step.CompletedAt)
	payload["action_ref"] = actionReference(step.Name)
	stableKey := facts.StableID(facts.CICDStepFactKind, map[string]any{
		"job_id":      providerID(job.ID),
		"run_attempt": defaultAttempt(providerID(run.RunAttempt)),
		"run_id":      providerID(run.ID),
		"step_number": stepNumber,
	})
	return newEnvelope(ctx, facts.CICDStepFactKind, stableKey, providerID(job.ID)+":"+stepNumber, payload)
}

func artifactEnvelope(ctx FixtureContext, run githubRun, artifact githubArtifact) facts.Envelope {
	artifactID := providerID(artifact.ID)
	payload := sharedPayload(ctx, run)
	payload["artifact_id"] = artifactID
	payload["artifact_name"] = trim(artifact.Name)
	payload["artifact_type"] = defaultArtifactType(artifact)
	payload["artifact_digest"] = trim(artifact.Digest)
	payload["size_bytes"] = artifact.SizeBytes
	payload["expired"] = artifact.Expired
	payload["created_at"] = trim(artifact.CreatedAt)
	payload["expires_at"] = trim(artifact.ExpiresAt)
	payload["download_url"] = stripSensitiveURL(artifact.ArchiveDownloadURL)
	payload["correlation_anchors"] = nonEmptyStrings(providerID(run.ID), trim(artifact.Digest))
	stableKey := facts.StableID(facts.CICDArtifactFactKind, map[string]any{
		"artifact_id": artifactID,
		"run_attempt": defaultAttempt(providerID(run.RunAttempt)),
		"run_id":      providerID(run.ID),
	})
	return newEnvelope(ctx, facts.CICDArtifactFactKind, stableKey, artifactID, payload)
}

func environmentEnvelope(ctx FixtureContext, run githubRun, job githubJob) facts.Envelope {
	payload := sharedPayload(ctx, run)
	payload["job_id"] = providerID(job.ID)
	payload["environment"] = trim(job.Environment)
	payload["deployment_status"] = trim(job.DeploymentStatus)
	stableKey := facts.StableID(facts.CICDEnvironmentObservationFactKind, map[string]any{
		"environment": trim(job.Environment),
		"job_id":      providerID(job.ID),
		"run_attempt": defaultAttempt(providerID(run.RunAttempt)),
		"run_id":      providerID(run.ID),
	})
	return newEnvelope(ctx, facts.CICDEnvironmentObservationFactKind, stableKey, providerID(job.ID)+":"+trim(job.Environment), payload)
}

func triggerEnvelope(ctx FixtureContext, run githubRun, trigger githubTrigger) facts.Envelope {
	payload := sharedPayload(ctx, run)
	payload["trigger_kind"] = trim(trigger.TriggerKind)
	payload["source_provider"] = trim(trigger.SourceProvider)
	payload["source_run_id"] = trim(trigger.SourceRunID)
	stableKey := facts.StableID(facts.CICDTriggerEdgeFactKind, map[string]any{
		"run_attempt":     defaultAttempt(providerID(run.RunAttempt)),
		"run_id":          providerID(run.ID),
		"source_provider": trim(trigger.SourceProvider),
		"source_run_id":   trim(trigger.SourceRunID),
		"trigger_kind":    trim(trigger.TriggerKind),
	})
	return newEnvelope(ctx, facts.CICDTriggerEdgeFactKind, stableKey, trim(trigger.SourceRunID), payload)
}

func warningEnvelope(ctx FixtureContext, run githubRun, reason, message string) facts.Envelope {
	reason = trim(reason)
	payload := sharedPayload(ctx, run)
	payload["reason"] = reason
	payload["message"] = trim(message)
	payload["partial_generation"] = true
	stableKey := facts.StableID(facts.CICDWarningFactKind, map[string]any{
		"reason":      reason,
		"run_attempt": defaultAttempt(providerID(run.RunAttempt)),
		"run_id":      providerID(run.ID),
	})
	return newEnvelope(ctx, facts.CICDWarningFactKind, stableKey, reason, payload)
}

func repositoryID(repository githubRepository) string {
	fullName := strings.Trim(strings.TrimSpace(repository.FullName), "/")
	if fullName == "" {
		return ""
	}
	return "github.com/" + fullName
}

func defaultArtifactType(artifact githubArtifact) string {
	if trim(artifact.ArtifactType) != "" {
		return trim(artifact.ArtifactType)
	}
	return "generic"
}

func actionReference(stepName string) string {
	stepName = strings.TrimPrefix(trim(stepName), "Run ")
	if strings.Contains(stepName, "@") && !strings.Contains(stepName, " ") {
		return stepName
	}
	return ""
}

func nonEmptyStrings(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := trim(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
