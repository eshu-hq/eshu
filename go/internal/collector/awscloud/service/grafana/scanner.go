// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package grafana

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits Amazon Managed Grafana metadata-only facts for one claimed
// account and region. It never creates, updates, or deletes a workspace, never
// mints a workspace API key or service-account token, and never reads or
// persists SAML/IAM Identity Center authentication secrets, dashboards, alert
// rules, or query results. It reports workspaces plus relationship edges to the
// workspace IAM role and, when a vpcConfiguration is present, to the workspace's
// VPC subnets and security groups.
type Scanner struct {
	// Client is the metadata-only Managed Grafana snapshot source.
	Client Client
}

// Scan observes Managed Grafana workspaces and the direct IAM role and VPC
// subnet/security-group dependency metadata through the configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("grafana scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceGrafana:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceGrafana
	default:
		return nil, fmt.Errorf("grafana scanner received service_kind %q", boundary.ServiceKind)
	}

	snapshot, err := s.Client.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Grafana workspaces: %w", err)
	}

	var envelopes []facts.Envelope
	if err := appendWarnings(&envelopes, snapshot.Warnings); err != nil {
		return nil, err
	}
	for _, workspace := range snapshot.Workspaces {
		next, err := workspaceEnvelopes(boundary, workspace)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	return envelopes, nil
}

func appendWarnings(envelopes *[]facts.Envelope, observations []awscloud.WarningObservation) error {
	for _, observation := range observations {
		envelope, err := awscloud.NewWarningEnvelope(observation)
		if err != nil {
			return err
		}
		*envelopes = append(*envelopes, envelope)
	}
	return nil
}

func workspaceEnvelopes(boundary awscloud.Boundary, workspace Workspace) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(workspaceObservation(boundary, workspace))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, relationship := range workspaceRelationships(boundary, workspace) {
		envelope, err := awscloud.NewRelationshipEnvelope(relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func workspaceObservation(boundary awscloud.Boundary, workspace Workspace) awscloud.ResourceObservation {
	arn := strings.TrimSpace(workspace.ARN)
	name := strings.TrimSpace(workspace.Name)
	resourceID := workspaceResourceID(workspace)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeGrafanaWorkspace,
		Name:         name,
		State:        strings.TrimSpace(workspace.Status),
		Tags:         cloneStringMap(workspace.Tags),
		Attributes: map[string]any{
			"workspace_id":              strings.TrimSpace(workspace.ID),
			"description":               strings.TrimSpace(workspace.Description),
			"grafana_version":           strings.TrimSpace(workspace.GrafanaVersion),
			"endpoint":                  strings.TrimSpace(workspace.Endpoint),
			"account_access_type":       strings.TrimSpace(workspace.AccountAccessType),
			"permission_type":           strings.TrimSpace(workspace.PermissionType),
			"workspace_role_arn":        strings.TrimSpace(workspace.WorkspaceRoleARN),
			"data_sources":              cloneStrings(workspace.DataSources),
			"notification_destinations": cloneStrings(workspace.NotificationDestinations),
			"authentication_providers":  cloneStrings(workspace.AuthenticationProviders),
			"subnet_ids":                cloneStrings(workspace.SubnetIDs),
			"security_group_ids":        cloneStrings(workspace.SecurityGroupIDs),
			"created":                   timeOrNil(workspace.Created),
			"modified":                  timeOrNil(workspace.Modified),
		},
		CorrelationAnchors: correlationAnchors(arn, workspace.ID, name),
		SourceRecordID:     resourceID,
	}
}

// correlationAnchors returns the distinct, non-empty identity anchors for a
// workspace so downstream correlation can join on the ARN, the bare workspace
// id, or the workspace name.
func correlationAnchors(values ...string) []string {
	anchors := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, candidate := range values {
		trimmed := strings.TrimSpace(candidate)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		anchors = append(anchors, trimmed)
	}
	if len(anchors) == 0 {
		return nil
	}
	return anchors
}
