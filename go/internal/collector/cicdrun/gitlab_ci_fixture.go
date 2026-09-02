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

// GitLabCIFixtureEnvelopes normalizes one fixture-shaped GitLab CI/CD
// pipeline+jobs payload into reported-confidence CI/CD facts. GitLab is a
// SECOND provider on the EXISTING ci.* fact contract GitHub Actions already
// populates: both emit the same FactKind/SchemaVersion constants and the same
// reducer join-key shape (provider, run_id, run_attempt --
// go/internal/reducer/cicdrun/ci_cd_run_correlation_decode.go's
// CICDRunKeyFromParts) -- see
// TestGitLabCIFixtureSharesFactKindsAndJoinKeyShapeWithGitHubActions.
//
// Scope is intentionally narrower than GitHub Actions:
//   - No ci.pipeline_definition: GitLab's Pipelines API exposes no stable
//     workflow-definition ID distinct from the pipeline itself -- pipeline.id
//     IS the run, and there is no separate "workflow" resource the way
//     GitHub Actions models workflow vs run.
//   - No ci.step: GitLab's Jobs API reports no step-level breakdown; a job is
//     the smallest unit GitLab exposes here.
//   - No ci.trigger_edge / ci.environment_observation: out of v1 scope,
//     matching ghactionsruntime's own live client, which also does not
//     populate RunSnapshot.Triggers or job.environment today.
func GitLabCIFixtureEnvelopes(raw []byte, ctx FixtureContext) ([]facts.Envelope, error) {
	if err := validateContext(ctx); err != nil {
		return nil, err
	}
	var fixture gitlabCIFixture
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&fixture); err != nil {
		return nil, fmt.Errorf("parse gitlab ci fixture: %w", err)
	}
	pipelineID, err := providerID(fixture.Pipeline.ID)
	if err != nil {
		return nil, fmt.Errorf("gitlab ci fixture pipeline.id: %w", err)
	}
	if pipelineID == "" {
		return nil, fmt.Errorf("gitlab ci fixture pipeline.id must not be blank")
	}

	envelopes := make([]facts.Envelope, 0, 2+len(fixture.Jobs))
	run, err := gitlabRunEnvelope(ctx, fixture.Pipeline)
	if err != nil {
		return nil, err
	}
	envelopes = append(envelopes, run)
	if gitlabRepositoryID(fixture.Pipeline, ctx) == "" || trim(fixture.Pipeline.SHA) == "" {
		warning, err := gitlabWarningEnvelope(ctx, fixture.Pipeline, "run:anchors", "run_missing_repository_or_commit", "run metadata omitted repository locator or commit SHA")
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, warning)
	}
	for jobIndex, job := range fixture.Jobs {
		jobID, err := providerID(job.ID)
		if err != nil || jobID == "" {
			warning, warningErr := gitlabWarningEnvelope(ctx, fixture.Pipeline, fmt.Sprintf("job:%d", jobIndex), "job_missing_id", "job metadata omitted provider job ID")
			if warningErr != nil {
				return nil, warningErr
			}
			envelopes = append(envelopes, warning)
			continue
		}
		jobFact, err := gitlabJobEnvelope(ctx, fixture.Pipeline, job)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, jobFact)
		for artifactIndex, artifact := range job.Artifacts {
			filename := trim(artifact.Filename)
			if filename == "" {
				warning, warningErr := gitlabWarningEnvelope(ctx, fixture.Pipeline, fmt.Sprintf("artifact:%s:%d", jobID, artifactIndex), "artifact_missing_id", "artifact metadata omitted provider filename")
				if warningErr != nil {
					return nil, warningErr
				}
				envelopes = append(envelopes, warning)
				continue
			}
			artifactID := jobID + ":" + filename
			artifactFact, err := gitlabArtifactEnvelope(ctx, fixture.Pipeline, jobID, artifactID, artifact)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, artifactFact)
			// GitLab's job artifacts list never carries a content digest
			// (see gitlabArtifact's doc comment in types.go), so every
			// emitted artifact is followed by this warning -- it is not a
			// fixture gap, it matches the real API shape.
			warning, err := gitlabWarningEnvelope(ctx, fixture.Pipeline, "artifact:"+artifactID, "artifact_missing_digest", "artifact metadata did not include a digest")
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, warning)
		}
	}
	if fixture.JobsPartial {
		warning, err := gitlabWarningEnvelope(ctx, fixture.Pipeline, "jobs:partial", "partial_jobs_payload", "job metadata was partial or unavailable")
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, warning)
	}
	return deduplicateEnvelopes(envelopes), nil
}

func gitlabSharedPayload(ctx FixtureContext, pipeline gitlabPipeline) (map[string]any, error) {
	pipelineID, err := providerID(pipeline.ID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"collector_instance_id": ctx.CollectorInstanceID,
		"provider":              string(ProviderGitLabCI),
		"run_id":                pipelineID,
		// GitLab pipelines have no in-place retry-attempt concept the way
		// GitHub Actions runs do: re-running a pipeline creates a NEW
		// pipeline ID rather than incrementing an attempt counter on the
		// same ID, so run_attempt is always "1" for GitLab-sourced facts.
		"run_attempt": "1",
	}, nil
}

func gitlabRunEnvelope(ctx FixtureContext, pipeline gitlabPipeline) (facts.Envelope, error) {
	pipelineID, err := providerID(pipeline.ID)
	if err != nil {
		return facts.Envelope{}, err
	}
	runNumber, err := providerID(pipeline.IID)
	if err != nil {
		return facts.Envelope{}, err
	}
	payload, err := gitlabSharedPayload(ctx, pipeline)
	if err != nil {
		return facts.Envelope{}, err
	}
	// GitLab's Pipelines API models a single terminal "status" enum
	// (success/failed/canceled/skipped/...) rather than GitHub Actions'
	// separate status (lifecycle: queued/in_progress/completed) and
	// conclusion (outcome: success/failure) fields, so both the shared
	// "status" and "result" payload keys mirror pipeline.status.
	status := trim(pipeline.Status)
	payload["run_number"] = runNumber
	payload["event"] = trim(pipeline.Source)
	payload["status"] = status
	payload["result"] = status
	payload["branch"] = trim(pipeline.Ref)
	payload["commit_sha"] = trim(pipeline.SHA)
	canonicalRepoID := gitlabRepositoryID(pipeline, ctx)
	providerRepoID := gitlabProviderRepositoryID(pipeline, ctx)
	repositoryURL := gitlabRepositoryCanonicalURL(pipeline, ctx)
	payload["repository_id"] = canonicalRepoID
	payload["provider_repository_id"] = providerRepoID
	payload["repository_url"] = repositoryURL
	payload["actor"] = trim(pipeline.User.Username)
	payload["started_at"] = trim(pipeline.StartedAt)
	payload["updated_at"] = trim(pipeline.UpdatedAt)
	payload["url"] = stripSensitiveURL(pipeline.WebURL)
	payload["correlation_anchors"] = nonEmptyStrings(canonicalRepoID, trim(pipeline.SHA), pipelineID)
	runAttempt, err := payloadStringField(payload, "run_attempt")
	if err != nil {
		return facts.Envelope{}, err
	}
	if err := mergeContractPayload(payload, func() (map[string]any, error) {
		return factschema.EncodeCICDRun(cicdrunv1.Run{
			Provider:             string(ProviderGitLabCI),
			RunID:                pipelineID,
			RunAttempt:           stringPtr(runAttempt),
			RunNumber:            stringPtr(runNumber),
			Event:                stringPtr(trim(pipeline.Source)),
			Status:               stringPtr(status),
			Result:               stringPtr(status),
			Branch:               stringPtr(trim(pipeline.Ref)),
			CommitSHA:            stringPtr(trim(pipeline.SHA)),
			RepositoryID:         stringPtr(canonicalRepoID),
			ProviderRepositoryID: stringPtr(providerRepoID),
			RepositoryURL:        stringPtr(repositoryURL),
			Actor:                stringPtr(trim(pipeline.User.Username)),
			StartedAt:            stringPtr(trim(pipeline.StartedAt)),
			UpdatedAt:            stringPtr(trim(pipeline.UpdatedAt)),
			URL:                  stringPtr(stripSensitiveURL(pipeline.WebURL)),
			CorrelationAnchors:   nonEmptyStrings(canonicalRepoID, trim(pipeline.SHA), pipelineID),
			CollectorInstanceID:  stringPtr(ctx.CollectorInstanceID),
		})
	}); err != nil {
		return facts.Envelope{}, err
	}
	stableKey := facts.StableID(facts.CICDRunFactKind, map[string]any{
		"provider":    ProviderGitLabCI,
		"run_attempt": payload["run_attempt"],
		"run_id":      pipelineID,
	})
	return newEnvelope(ctx, facts.CICDRunFactKind, stableKey, pipelineID, payload), nil
}

func gitlabJobEnvelope(ctx FixtureContext, pipeline gitlabPipeline, job gitlabJob) (facts.Envelope, error) {
	jobID, err := providerID(job.ID)
	if err != nil {
		return facts.Envelope{}, err
	}
	payload, err := gitlabSharedPayload(ctx, pipeline)
	if err != nil {
		return facts.Envelope{}, err
	}
	payload["job_id"] = jobID
	payload["job_name"] = trim(job.Name)
	payload["stage"] = trim(job.Stage)
	payload["status"] = trim(job.Status)
	payload["result"] = trim(job.Status)
	payload["started_at"] = trim(job.StartedAt)
	payload["completed_at"] = trim(job.FinishedAt)
	stableKey := facts.StableID(facts.CICDJobFactKind, map[string]any{
		"job_id":      jobID,
		"run_attempt": payload["run_attempt"],
		"run_id":      payload["run_id"],
	})
	return newEnvelope(ctx, facts.CICDJobFactKind, stableKey, jobID, payload), nil
}

func gitlabArtifactEnvelope(ctx FixtureContext, pipeline gitlabPipeline, jobID, artifactID string, artifact gitlabArtifact) (facts.Envelope, error) {
	filename := trim(artifact.Filename)
	payload, err := gitlabSharedPayload(ctx, pipeline)
	if err != nil {
		return facts.Envelope{}, err
	}
	payload["job_id"] = jobID
	payload["artifact_id"] = artifactID
	payload["artifact_name"] = filename
	payload["artifact_type"] = gitlabArtifactType(artifact)
	payload["artifact_digest"] = ""
	payload["size_bytes"] = artifact.Size
	runID, err := payloadStringField(payload, "run_id")
	if err != nil {
		return facts.Envelope{}, err
	}
	runAttempt, err := payloadStringField(payload, "run_attempt")
	if err != nil {
		return facts.Envelope{}, err
	}
	payload["correlation_anchors"] = nonEmptyStrings(runID, jobID)
	if err := mergeContractPayload(payload, func() (map[string]any, error) {
		return factschema.EncodeCICDArtifact(cicdrunv1.Artifact{
			Provider:            string(ProviderGitLabCI),
			RunID:               runID,
			RunAttempt:          stringPtr(runAttempt),
			ArtifactID:          stringPtr(artifactID),
			ArtifactName:        stringPtr(filename),
			ArtifactType:        stringPtr(gitlabArtifactType(artifact)),
			ArtifactDigest:      stringPtr(""),
			SizeBytes:           int64Ptr(artifact.Size),
			CorrelationAnchors:  nonEmptyStrings(runID, jobID),
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

func gitlabWarningEnvelope(ctx FixtureContext, pipeline gitlabPipeline, warningKey, reason, message string) (facts.Envelope, error) {
	reason = trim(reason)
	payload, err := gitlabSharedPayload(ctx, pipeline)
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
