// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codeguru

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits Amazon CodeGuru metadata-only facts for one claimed account and
// region. It covers CodeGuru Reviewer repository associations and CodeGuru
// Profiler profiling groups. It never reads recommendation content, code-review
// findings, profiling sample data, flame graphs, or agent telemetry, and never
// mutates any CodeGuru resource. It reports associations and profiling groups
// plus the association-reviews-CodeCommit-repository relationship.
type Scanner struct {
	// Client is the metadata-only CodeGuru snapshot source.
	Client Client
}

// Scan observes CodeGuru Reviewer repository associations and CodeGuru Profiler
// profiling groups plus the CodeCommit repository edge through the configured
// client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("codeguru scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceCodeGuru:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceCodeGuru
	default:
		return nil, fmt.Errorf("codeguru scanner received service_kind %q", boundary.ServiceKind)
	}

	snapshot, err := s.Client.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("snapshot CodeGuru metadata: %w", err)
	}
	var envelopes []facts.Envelope
	if err := appendWarnings(&envelopes, snapshot.Warnings); err != nil {
		return nil, err
	}
	for _, association := range snapshot.RepositoryAssociations {
		next, err := associationEnvelopes(boundary, association)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	for _, group := range snapshot.ProfilingGroups {
		resource, err := awscloud.NewResourceEnvelope(profilingGroupObservation(boundary, group))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
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

func associationEnvelopes(
	boundary awscloud.Boundary,
	association RepositoryAssociation,
) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(associationObservation(boundary, association))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if relationship := associationCodeCommitRelationship(boundary, association); relationship != nil {
		envelope, err := awscloud.NewRelationshipEnvelope(*relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func associationObservation(
	boundary awscloud.Boundary,
	association RepositoryAssociation,
) awscloud.ResourceObservation {
	arn := strings.TrimSpace(association.ARN)
	name := strings.TrimSpace(association.Name)
	resourceID := associationResourceID(association)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeCodeGuruRepositoryAssociation,
		Name:         name,
		State:        strings.TrimSpace(association.State),
		Tags:         cloneStringMap(association.Tags),
		Attributes: map[string]any{
			"association_id":    strings.TrimSpace(association.AssociationID),
			"provider_type":     strings.TrimSpace(association.ProviderType),
			"owner":             strings.TrimSpace(association.Owner),
			"connection_arn":    strings.TrimSpace(association.ConnectionARN),
			"s3_bucket_name":    strings.TrimSpace(association.S3BucketName),
			"kms_key_id":        strings.TrimSpace(association.KMSKeyID),
			"encryption_option": strings.TrimSpace(association.EncryptionOption),
			"created_at":        timeOrNil(association.CreatedAt),
			"last_updated_at":   timeOrNil(association.LastUpdatedAt),
		},
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     resourceID,
	}
}

func profilingGroupObservation(
	boundary awscloud.Boundary,
	group ProfilingGroup,
) awscloud.ResourceObservation {
	arn := strings.TrimSpace(group.ARN)
	name := strings.TrimSpace(group.Name)
	resourceID := profilingGroupResourceID(group)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeCodeGuruProfilingGroup,
		Name:         name,
		Tags:         cloneStringMap(group.Tags),
		Attributes: map[string]any{
			"compute_platform":  strings.TrimSpace(group.ComputePlatform),
			"profiling_enabled": boolOrNil(group.ProfilingEnabled),
			"created_at":        timeOrNil(group.CreatedAt),
			"last_updated_at":   timeOrNil(group.LastUpdatedAt),
		},
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     resourceID,
	}
}
