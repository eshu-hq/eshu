package cicdrun

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
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
	payload["repository_id"] = repositoryID(run.Repository, ctx)
	payload["repository_url"] = trim(run.Repository.HTMLURL)
	payload["actor"] = trim(run.Actor.Login)
	payload["started_at"] = trim(run.RunStartedAt)
	payload["updated_at"] = trim(run.UpdatedAt)
	payload["url"] = stripSensitiveURL(run.HTMLURL)
	payload["correlation_anchors"] = nonEmptyStrings(repositoryID(run.Repository, ctx), trim(run.HeadSHA), runID)
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

func repositoryID(repository githubRepository, ctx FixtureContext) string {
	fullName := strings.Trim(strings.TrimSpace(repository.FullName), "/")
	if fullName == "" {
		return ""
	}
	return repositoryHost(repository, ctx) + "/" + fullName
}

func repositoryHost(repository githubRepository, ctx FixtureContext) string {
	for _, rawURL := range []string{repository.HTMLURL, ctx.SourceURI, ctx.ScopeID} {
		parsed, err := url.Parse(rawURL)
		if err == nil && parsed.Host != "" {
			return parsed.Host
		}
	}
	return "github.com"
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

func artifactMatchesRun(run githubRun, artifact githubArtifact) bool {
	if artifact.WorkflowRun.ID != nil {
		artifactRunID, err := providerID(artifact.WorkflowRun.ID)
		if err != nil {
			return false
		}
		runID, err := providerID(run.ID)
		if err != nil || artifactRunID != "" && artifactRunID != runID {
			return false
		}
	}
	if trim(artifact.WorkflowRun.HeadSHA) != "" && trim(run.HeadSHA) != "" && trim(artifact.WorkflowRun.HeadSHA) != trim(run.HeadSHA) {
		return false
	}
	return true
}

func deduplicateEnvelopes(envelopes []facts.Envelope) []facts.Envelope {
	seen := make(map[string]bool, len(envelopes))
	out := make([]facts.Envelope, 0, len(envelopes))
	for _, envelope := range envelopes {
		if seen[envelope.FactID] {
			continue
		}
		seen[envelope.FactID] = true
		out = append(out, envelope)
	}
	return out
}
