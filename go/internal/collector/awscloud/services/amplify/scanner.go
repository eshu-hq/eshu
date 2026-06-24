// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package amplify

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS Amplify metadata-only facts for one claimed account and
// region. It never creates, updates, deletes, starts a job, or starts a
// deployment, and it never persists Amplify environment variables, build-spec
// secrets, or repository access tokens. Repository URLs are reduced to host and
// path only so an embedded token cannot leak through a fact payload or a graph
// join key.
type Scanner struct {
	// Client is the metadata-only Amplify read surface.
	Client Client
}

// Scan observes Amplify apps, their branches, and their custom-domain
// associations through the configured client. It returns one aws_resource fact
// per app and branch plus aws_relationship facts for the app->repository,
// app->IAM-role, app->custom-domain (Route 53 / CloudFront), and branch->app
// edges Amplify reports. Environment variables, build-spec bodies, basic-auth
// credentials, and repository access tokens stay outside the scanner contract.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("amplify scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceAmplify:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceAmplify
	default:
		return nil, fmt.Errorf("amplify scanner received service_kind %q", boundary.ServiceKind)
	}

	apps, err := s.Client.ListApps(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Amplify apps: %w", err)
	}

	var envelopes []facts.Envelope
	for _, app := range apps {
		appEnvelopes, err := s.scanApp(ctx, boundary, app)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, appEnvelopes...)
	}

	return envelopes, nil
}

func (s Scanner) scanApp(
	ctx context.Context,
	boundary awscloud.Boundary,
	app App,
) ([]facts.Envelope, error) {
	appID := appResourceID(boundary, app)

	resource, err := awscloud.NewResourceEnvelope(appObservation(boundary, app))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}

	domains, err := s.Client.ListDomainAssociations(ctx, app.ID)
	if err != nil {
		return nil, fmt.Errorf("list Amplify domain associations for %q: %w", app.ID, err)
	}
	for _, observation := range appRelationships(boundary, app, domains) {
		relEnvelope, err := awscloud.NewRelationshipEnvelope(observation)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, relEnvelope)
	}

	branches, err := s.Client.ListBranches(ctx, app.ID)
	if err != nil {
		return nil, fmt.Errorf("list Amplify branches for %q: %w", app.ID, err)
	}
	for _, branch := range branches {
		if strings.TrimSpace(branch.AppID) == "" {
			branch.AppID = app.ID
		}
		branchResource, err := awscloud.NewResourceEnvelope(branchObservation(boundary, branch))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, branchResource)
		if rel, ok := branchAppRelationship(boundary, branch, appID); ok {
			relEnvelope, err := awscloud.NewRelationshipEnvelope(rel)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, relEnvelope)
		}
	}

	return envelopes, nil
}

func appObservation(boundary awscloud.Boundary, app App) awscloud.ResourceObservation {
	arn := firstNonEmpty(app.ARN, appARN(boundary, app.ID))
	resourceID := appResourceID(boundary, app)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeAmplifyApp,
		Name:         strings.TrimSpace(app.Name),
		Tags:         cloneStringMap(app.Tags),
		Attributes: map[string]any{
			"app_id":                  strings.TrimSpace(app.ID),
			"platform":                strings.TrimSpace(app.Platform),
			"repository_url":          strings.TrimSpace(app.RepositoryURL),
			"repository_clone_method": strings.TrimSpace(app.RepositoryCloneMethod),
			"default_domain":          strings.TrimSpace(app.DefaultDomain),
			"service_role_arn":        strings.TrimSpace(app.ServiceRoleARN),
			"compute_role_arn":        strings.TrimSpace(app.ComputeRoleARN),
			"production_branch":       strings.TrimSpace(app.ProductionBranchName),
			"create_time":             timeOrNil(app.CreateTime),
			"update_time":             timeOrNil(app.UpdateTime),
		},
		CorrelationAnchors: []string{arn, app.ID, app.DefaultDomain},
		SourceRecordID:     resourceID,
	}
}

func branchObservation(boundary awscloud.Boundary, branch Branch) awscloud.ResourceObservation {
	arn := firstNonEmpty(branch.ARN, branchARN(boundary, branch.AppID, branch.Name))
	resourceID := branchResourceID(boundary, branch)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeAmplifyBranch,
		Name:         strings.TrimSpace(branch.Name),
		State:        strings.TrimSpace(branch.Stage),
		Tags:         cloneStringMap(branch.Tags),
		Attributes: map[string]any{
			"app_id":              strings.TrimSpace(branch.AppID),
			"branch_name":         strings.TrimSpace(branch.Name),
			"display_name":        strings.TrimSpace(branch.DisplayName),
			"stage":               strings.TrimSpace(branch.Stage),
			"framework":           strings.TrimSpace(branch.Framework),
			"enable_auto_build":   branch.EnableAutoBuild,
			"compute_role_arn":    strings.TrimSpace(branch.ComputeRoleARN),
			"custom_domain_count": branch.CustomDomainCount,
			"create_time":         timeOrNil(branch.CreateTime),
			"update_time":         timeOrNil(branch.UpdateTime),
		},
		CorrelationAnchors: []string{arn, branch.Name},
		SourceRecordID:     resourceID,
	}
}
