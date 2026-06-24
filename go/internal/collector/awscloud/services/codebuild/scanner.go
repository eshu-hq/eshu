// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codebuild

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// Scanner emits AWS CodeBuild metadata facts for one claimed account and
// region. It never reads or persists buildspec.yml bodies, environment-variable
// PLAINTEXT values, build logs, or source-credential tokens. PLAINTEXT
// environment-variable values are redacted through the configured redaction key
// before they reach scanner records, so the scan fails closed when that key is
// zero.
type Scanner struct {
	// Client is the metadata-only CodeBuild read surface.
	Client Client
	// RedactionKey produces deterministic markers for environment-variable
	// PLAINTEXT values, which may carry secrets. Scan fails closed when it is
	// zero so raw values cannot leak.
	RedactionKey redact.Key
}

// Scan observes CodeBuild build projects, report groups, and recent builds
// through the configured client. It returns one aws_resource fact per resource
// plus aws_relationship facts for the project edges CodeBuild reports directly.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("codebuild scanner client is required")
	}
	if s.RedactionKey.IsZero() {
		return nil, fmt.Errorf("codebuild scanner redaction key is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceCodeBuild:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceCodeBuild
	default:
		return nil, fmt.Errorf("codebuild scanner received service_kind %q", boundary.ServiceKind)
	}

	var envelopes []facts.Envelope

	projects, err := s.Client.ListProjects(ctx)
	if err != nil {
		return nil, fmt.Errorf("list CodeBuild projects: %w", err)
	}
	for _, project := range projects {
		resource, err := awscloud.NewResourceEnvelope(projectObservation(boundary, project))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)

		for _, observation := range projectRelationships(boundary, project) {
			relationship, err := awscloud.NewRelationshipEnvelope(observation)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, relationship)
		}
	}

	reportGroups, err := s.Client.ListReportGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("list CodeBuild report groups: %w", err)
	}
	for _, group := range reportGroups {
		resource, err := awscloud.NewResourceEnvelope(reportGroupObservation(boundary, group))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
	}

	builds, err := s.Client.ListRecentBuilds(ctx)
	if err != nil {
		return nil, fmt.Errorf("list CodeBuild recent builds: %w", err)
	}
	for _, build := range builds {
		resource, err := awscloud.NewResourceEnvelope(buildObservation(boundary, build))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
	}

	return envelopes, nil
}
