// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package resourcegroups

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS Resource Groups metadata-only facts for one claimed account
// and region.
//
// Scanner never reads or persists the resource-query body of a group (the
// tag-filter expression or CloudFormation template JSON); it records the query
// type only. Mutation APIs (CreateGroup, UpdateGroup, DeleteGroup,
// UpdateGroupQuery, GroupResources, UngroupResources, Tag, Untag,
// PutGroupConfiguration) are unreachable from the scanner because the Client
// interface does not expose them.
type Scanner struct {
	Client Client
}

// Scan observes AWS Resource Groups metadata for one boundary and returns
// reported-confidence AWS facts: one resource fact per group, one membership
// edge per recognized member resource, and one group-to-stack edge for each
// CloudFormation-stack-backed group. Members whose resource family the
// classifier does not recognize are skipped rather than emitted with an empty
// target type.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("resourcegroups scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceResourceGroups:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceResourceGroups
	default:
		return nil, fmt.Errorf("resourcegroups scanner received service_kind %q", boundary.ServiceKind)
	}

	groups, err := s.Client.ListGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("list AWS Resource Groups: %w", err)
	}

	var envelopes []facts.Envelope
	for _, group := range groups {
		next, err := groupEnvelopes(boundary, group)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	return envelopes, nil
}

// groupEnvelopes projects one group into its resource fact plus the membership
// and stack-backing relationship facts it anchors.
func groupEnvelopes(boundary awscloud.Boundary, group Group) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(groupObservation(boundary, group))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}

	if rel, ok := groupBackedByStackRelationship(boundary, group); ok {
		relEnvelope, err := awscloud.NewRelationshipEnvelope(rel)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, relEnvelope)
	}

	for _, member := range group.Members {
		rel, ok := groupContainsMemberRelationship(boundary, group, member)
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

// groupObservation projects one group into a metadata-only resource
// observation. The resource-query body is never persisted; only the query type
// is recorded. The group ARN is the durable identity and the partition is taken
// from it, never synthesized.
func groupObservation(boundary awscloud.Boundary, group Group) awscloud.ResourceObservation {
	groupARN := strings.TrimSpace(group.ARN)
	name := strings.TrimSpace(group.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          groupARN,
		ResourceID:   firstNonEmpty(groupARN, name),
		ResourceType: awscloud.ResourceTypeResourceGroupsGroup,
		Name:         name,
		Attributes: map[string]any{
			"description":      strings.TrimSpace(group.Description),
			"query_type":       strings.TrimSpace(group.QueryType),
			"stack_identifier": strings.TrimSpace(group.StackIdentifier),
			"member_count":     len(group.Members),
			"creation_time":    timeOrNil(group.CreationTime),
		},
		CorrelationAnchors: []string{groupARN, name},
		SourceRecordID:     firstNonEmpty(groupARN, name),
	}
}
