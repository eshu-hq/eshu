// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package amp

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits Amazon Managed Service for Prometheus metadata-only facts for
// one claimed account and region. It never reads ingested time-series samples,
// query results, alert-manager definitions, rule-group definition bodies, or
// scrape-configuration bodies, and never mutates AMP state. It reports
// workspaces, rule-groups namespaces (names only), and managed-collector
// scrapers, plus the workspace-to-KMS-key, namespace-in-workspace,
// scraper-to-EKS-cluster, scraper-to-workspace, scraper-to-subnet, and
// scraper-to-security-group relationships.
type Scanner struct {
	// Client is the metadata-only AMP snapshot source.
	Client Client
}

// Scan observes AMP workspaces, their rule-groups namespaces, and the account's
// scrapers plus their direct EKS, workspace, and VPC dependency metadata through
// the configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("amp scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceAMP:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceAMP
	default:
		return nil, fmt.Errorf("amp scanner received service_kind %q", boundary.ServiceKind)
	}

	snapshot, err := s.Client.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("snapshot AMP metadata: %w", err)
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
	for _, scraper := range snapshot.Scrapers {
		next, err := scraperEnvelopes(boundary, scraper)
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
	if relationship := workspaceKMSRelationship(boundary, workspace); relationship != nil {
		envelope, err := awscloud.NewRelationshipEnvelope(*relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	workspaceID := workspaceResourceID(workspace)
	for _, namespace := range workspace.RuleGroupsNamespaces {
		next, err := namespaceEnvelopes(boundary, workspaceID, namespace)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	return envelopes, nil
}

func namespaceEnvelopes(
	boundary awscloud.Boundary,
	workspaceID string,
	namespace RuleGroupsNamespace,
) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(namespaceObservation(boundary, workspaceID, namespace))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if relationship := namespaceInWorkspaceRelationship(boundary, workspaceID, namespace); relationship != nil {
		envelope, err := awscloud.NewRelationshipEnvelope(*relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func scraperEnvelopes(boundary awscloud.Boundary, scraper Scraper) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(scraperObservation(boundary, scraper))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, relationship := range scraperRelationships(boundary, scraper) {
		envelope, err := awscloud.NewRelationshipEnvelope(relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func workspaceObservation(boundary awscloud.Boundary, workspace Workspace) awscloud.ResourceObservation {
	workspaceARN := strings.TrimSpace(workspace.ARN)
	resourceID := workspaceResourceID(workspace)
	alias := strings.TrimSpace(workspace.Alias)
	name := firstNonEmpty(alias, workspace.WorkspaceID, workspaceARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          workspaceARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeAMPWorkspace,
		Name:         name,
		State:        strings.TrimSpace(workspace.Status),
		Tags:         cloneStringMap(workspace.Tags),
		Attributes: map[string]any{
			"workspace_id":  strings.TrimSpace(workspace.WorkspaceID),
			"alias":         alias,
			"kms_key_arn":   strings.TrimSpace(workspace.KMSKeyARN),
			"creation_time": timeOrNil(workspace.CreatedAt),
		},
		CorrelationAnchors: []string{workspaceARN, strings.TrimSpace(workspace.WorkspaceID)},
		SourceRecordID:     resourceID,
	}
}

func namespaceObservation(
	boundary awscloud.Boundary,
	workspaceID string,
	namespace RuleGroupsNamespace,
) awscloud.ResourceObservation {
	namespaceARN := strings.TrimSpace(namespace.ARN)
	name := strings.TrimSpace(namespace.Name)
	resourceID := namespaceResourceID(namespace)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          namespaceARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeAMPRuleGroupsNamespace,
		Name:         name,
		State:        strings.TrimSpace(namespace.Status),
		Tags:         cloneStringMap(namespace.Tags),
		Attributes: map[string]any{
			"namespace_name":    name,
			"workspace_id":      strings.TrimSpace(workspaceID),
			"creation_time":     timeOrNil(namespace.CreatedAt),
			"last_updated_time": timeOrNil(namespace.ModifiedAt),
		},
		CorrelationAnchors: []string{namespaceARN, name},
		SourceRecordID:     resourceID,
	}
}

func scraperObservation(boundary awscloud.Boundary, scraper Scraper) awscloud.ResourceObservation {
	scraperARN := strings.TrimSpace(scraper.ARN)
	resourceID := scraperResourceID(scraper)
	alias := strings.TrimSpace(scraper.Alias)
	name := firstNonEmpty(alias, scraper.ScraperID, scraperARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          scraperARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeAMPScraper,
		Name:         name,
		State:        strings.TrimSpace(scraper.Status),
		Tags:         cloneStringMap(scraper.Tags),
		Attributes: map[string]any{
			"scraper_id":                strings.TrimSpace(scraper.ScraperID),
			"alias":                     alias,
			"role_arn":                  strings.TrimSpace(scraper.RoleARN),
			"source_eks_cluster_arn":    strings.TrimSpace(scraper.SourceEKSClusterARN),
			"destination_workspace_arn": strings.TrimSpace(scraper.DestinationWorkspaceARN),
			"subnet_ids":                cloneStrings(scraper.SubnetIDs),
			"security_group_ids":        cloneStrings(scraper.SecurityGroupIDs),
			"creation_time":             timeOrNil(scraper.CreatedAt),
		},
		CorrelationAnchors: []string{scraperARN, strings.TrimSpace(scraper.ScraperID)},
		SourceRecordID:     resourceID,
	}
}
