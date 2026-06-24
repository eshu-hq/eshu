// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codedeploy

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// Scanner emits AWS CodeDeploy metadata facts for one claimed account and
// region. It never reads or persists appspec.yml bodies and redacts
// on-premises instance tag values through the configured redaction key.
type Scanner struct {
	// Client is the metadata-only CodeDeploy read surface.
	Client Client
	// RedactionKey produces deterministic markers for on-premises tag filter
	// values, which may match customer-PII patterns. Scan fails closed when it
	// is zero so raw values cannot leak.
	RedactionKey redact.Key
}

// Scan observes CodeDeploy applications, deployment groups, deployment configs,
// and recent deployments through the configured client. It returns one
// aws_resource fact per resource plus aws_relationship facts for the
// deployment-group edges CodeDeploy reports directly.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("codedeploy scanner client is required")
	}
	if s.RedactionKey.IsZero() {
		return nil, fmt.Errorf("codedeploy scanner redaction key is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceCodeDeploy:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceCodeDeploy
	default:
		return nil, fmt.Errorf("codedeploy scanner received service_kind %q", boundary.ServiceKind)
	}

	var envelopes []facts.Envelope

	applications, err := s.Client.ListApplications(ctx)
	if err != nil {
		return nil, fmt.Errorf("list CodeDeploy applications: %w", err)
	}
	for _, application := range applications {
		resource, err := awscloud.NewResourceEnvelope(applicationObservation(boundary, application))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)

		groupEnvelopes, err := s.scanDeploymentGroups(ctx, boundary, application.Name)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, groupEnvelopes...)
	}

	configs, err := s.Client.ListDeploymentConfigs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list CodeDeploy deployment configs: %w", err)
	}
	for _, config := range configs {
		resource, err := awscloud.NewResourceEnvelope(deploymentConfigObservation(boundary, config))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
	}

	deployments, err := s.Client.ListRecentDeployments(ctx)
	if err != nil {
		return nil, fmt.Errorf("list CodeDeploy recent deployments: %w", err)
	}
	for _, deployment := range deployments {
		resource, err := awscloud.NewResourceEnvelope(deploymentObservation(boundary, deployment))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
	}

	return envelopes, nil
}

func (s Scanner) scanDeploymentGroups(
	ctx context.Context,
	boundary awscloud.Boundary,
	applicationName string,
) ([]facts.Envelope, error) {
	groups, err := s.Client.ListDeploymentGroups(ctx, applicationName)
	if err != nil {
		return nil, fmt.Errorf("list CodeDeploy deployment groups for %q: %w", applicationName, err)
	}
	var envelopes []facts.Envelope
	for _, group := range groups {
		resource, err := awscloud.NewResourceEnvelope(deploymentGroupObservation(boundary, group))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)

		relationships, err := relationshipEnvelopes(boundary, group)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, relationships...)
	}
	return envelopes, nil
}

func relationshipEnvelopes(
	boundary awscloud.Boundary,
	group DeploymentGroup,
) ([]facts.Envelope, error) {
	var envelopes []facts.Envelope
	for _, observation := range deploymentGroupRelationships(boundary, group) {
		envelope, err := awscloud.NewRelationshipEnvelope(observation)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}
