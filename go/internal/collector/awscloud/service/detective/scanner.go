// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package detective

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits Amazon Detective metadata facts for one claimed account and
// region. It reports behavior graphs and their member accounts only. It never
// reads investigations, finding groups, indicators, or member contact emails,
// and it never mutates a Detective resource.
type Scanner struct {
	Client Client
}

// Scan observes Detective behavior graphs and their member accounts through the
// configured client, emitting one resource per graph and per member plus the
// graph-to-member-account and graph-to-GuardDuty-detector relationships.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("detective scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceDetective:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceDetective
	default:
		return nil, fmt.Errorf("detective scanner received service_kind %q", boundary.ServiceKind)
	}

	graphs, err := s.Client.ListGraphs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Detective behavior graphs: %w", err)
	}
	var envelopes []facts.Envelope
	for _, graph := range graphs {
		graphEnvelopes, err := s.graphEnvelopes(ctx, boundary, graph)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, graphEnvelopes...)
	}
	return envelopes, nil
}

// graphEnvelopes emits the resource and relationship facts for one behavior
// graph and its members. The graph node's resource id is the graph ARN, and
// every outgoing edge is sourced on that same ARN so the graph's edges join the
// graph node it publishes.
func (s Scanner) graphEnvelopes(
	ctx context.Context,
	boundary awscloud.Boundary,
	graph Graph,
) ([]facts.Envelope, error) {
	graphARN := strings.TrimSpace(graph.ARN)
	if graphARN == "" {
		// A graph with no ARN has no stable identity and cannot anchor edges;
		// skip it rather than key on list order.
		return nil, nil
	}

	tags, err := s.Client.ListTags(ctx, graphARN)
	if err != nil {
		return nil, fmt.Errorf("list Detective graph tags for %q: %w", graphARN, err)
	}
	members, err := s.Client.ListMembers(ctx, graphARN)
	if err != nil {
		return nil, fmt.Errorf("list Detective members for %q: %w", graphARN, err)
	}

	graphResource, err := awscloud.NewResourceEnvelope(graphObservation(boundary, graph, tags, members))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{graphResource}

	if rel, ok := guardDutyDetectorRelationship(boundary, graph); ok {
		envelope, err := awscloud.NewRelationshipEnvelope(rel)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}

	for _, member := range members {
		memberResource, err := awscloud.NewResourceEnvelope(memberObservation(boundary, graphARN, member))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, memberResource)
		rel, ok := memberAccountRelationship(boundary, graphARN, member)
		if !ok {
			continue
		}
		relEnvelope, err := awscloud.NewRelationshipEnvelope(rel)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, relEnvelope)
	}
	return envelopes, nil
}

// graphObservation builds the behavior graph resource. The graph ARN is both
// the ARN and the resource id, so the graph's edges (sourced on the ARN) join
// this node, and the partition is inherited from the ARN Detective reported.
func graphObservation(
	boundary awscloud.Boundary,
	graph Graph,
	tags map[string]string,
	members []MemberAccount,
) awscloud.ResourceObservation {
	graphARN := strings.TrimSpace(graph.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          graphARN,
		ResourceID:   graphARN,
		ResourceType: awscloud.ResourceTypeDetectiveGraph,
		Name:         graphName(graphARN),
		Tags:         cloneStringMap(tags),
		Attributes: map[string]any{
			"created_at":             strings.TrimSpace(graph.CreatedAt),
			"member_account_count":   len(members),
			"datasource_packages":    graphDatasourcePackages(members),
			"guardduty_detector_id":  strings.TrimSpace(graph.GuardDutyDetectorID),
			"sources_guardduty_data": graphSourcesGuardDuty(members, graph.GuardDutyDetectorID),
		},
		CorrelationAnchors: []string{graphARN},
		SourceRecordID:     graphARN,
	}
}

// memberObservation builds one member-account resource. The resource id is
// derived from the graph ARN and the member account id, never from list order,
// so the identity is stable across scans even if Detective reorders members.
func memberObservation(
	boundary awscloud.Boundary,
	graphARN string,
	member MemberAccount,
) awscloud.ResourceObservation {
	accountID := strings.TrimSpace(member.AccountID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   memberResourceID(graphARN, accountID),
		ResourceType: awscloud.ResourceTypeDetectiveMemberAccount,
		Name:         accountID,
		State:        strings.TrimSpace(member.Status),
		Attributes: map[string]any{
			"account_id":          accountID,
			"administrator_id":    strings.TrimSpace(member.AdministratorID),
			"graph_arn":           graphARN,
			"membership_status":   strings.TrimSpace(member.Status),
			"invitation_type":     strings.TrimSpace(member.InvitationType),
			"invited_at":          strings.TrimSpace(member.InvitedAt),
			"updated_at":          strings.TrimSpace(member.UpdatedAt),
			"datasource_packages": cloneStringSlice(member.DatasourcePackages),
		},
		CorrelationAnchors: []string{accountID},
		SourceRecordID:     memberResourceID(graphARN, accountID),
	}
}
