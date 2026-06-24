// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codepipeline

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// Scanner emits AWS CodePipeline metadata facts for one claimed account and
// region. It never persists action configuration values, webhook
// authentication secret tokens, or GitHub source-action OAuth tokens. Source
// revision commit-message summaries are redacted by the SDK adapter, so the
// scanner fails closed when the redaction key is zero.
type Scanner struct {
	// Client is the metadata-only CodePipeline read surface.
	Client Client
	// RedactionKey produces deterministic markers for source-revision summaries,
	// which may echo developer-pasted secrets. Scan fails closed when it is zero
	// so raw summaries cannot leak.
	RedactionKey redact.Key
}

// Scan observes CodePipeline pipelines, their recent executions, webhooks, and
// custom action types through the configured client. It returns one
// aws_resource fact per resource plus aws_relationship facts for the pipeline,
// stage, and action edges CodePipeline reports.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("codepipeline scanner client is required")
	}
	if s.RedactionKey.IsZero() {
		return nil, fmt.Errorf("codepipeline scanner redaction key is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceCodePipeline:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceCodePipeline
	default:
		return nil, fmt.Errorf("codepipeline scanner received service_kind %q", boundary.ServiceKind)
	}

	var envelopes []facts.Envelope

	pipelines, err := s.Client.ListPipelines(ctx)
	if err != nil {
		return nil, fmt.Errorf("list CodePipeline pipelines: %w", err)
	}
	for _, pipeline := range pipelines {
		pipelineEnvelopes, err := s.scanPipeline(ctx, boundary, pipeline)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, pipelineEnvelopes...)
	}

	webhooks, err := s.Client.ListWebhooks(ctx)
	if err != nil {
		return nil, fmt.Errorf("list CodePipeline webhooks: %w", err)
	}
	for _, webhook := range webhooks {
		resource, err := awscloud.NewResourceEnvelope(webhookObservation(boundary, webhook))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
		if rel, ok := webhookRelationship(boundary, webhook); ok {
			relEnvelope, err := awscloud.NewRelationshipEnvelope(rel)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, relEnvelope)
		}
	}

	actionTypes, err := s.Client.ListCustomActionTypes(ctx)
	if err != nil {
		return nil, fmt.Errorf("list CodePipeline custom action types: %w", err)
	}
	for _, actionType := range actionTypes {
		resource, err := awscloud.NewResourceEnvelope(actionTypeObservation(boundary, actionType))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
	}

	return envelopes, nil
}

func (s Scanner) scanPipeline(
	ctx context.Context,
	boundary awscloud.Boundary,
	pipeline Pipeline,
) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(pipelineObservation(boundary, pipeline))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}

	for _, observation := range pipelineRelationships(boundary, pipeline) {
		relEnvelope, err := awscloud.NewRelationshipEnvelope(observation)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, relEnvelope)
	}

	executions, err := s.Client.ListRecentExecutions(ctx, pipeline.Name)
	if err != nil {
		return nil, fmt.Errorf("list CodePipeline executions for %q: %w", pipeline.Name, err)
	}
	for _, execution := range executions {
		executionResource, err := awscloud.NewResourceEnvelope(executionObservation(boundary, pipeline, execution))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, executionResource)
	}

	return envelopes, nil
}
