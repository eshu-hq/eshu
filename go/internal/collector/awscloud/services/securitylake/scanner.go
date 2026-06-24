// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package securitylake

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits Amazon Security Lake metadata-only facts for one claimed account
// and Region. It never reads ingested security log records, object contents,
// subscriber credentials, or any data-plane payload, and never mutates Security
// Lake state. It reports data lakes, log sources, and subscribers plus the
// data-lake-to-S3/KMS/Lake-Formation, log-source-in-data-lake,
// log-source-to-IAM-role, and subscriber-to-IAM-role/S3 relationships.
type Scanner struct {
	// Client is the metadata-only Security Lake snapshot source.
	Client Client
}

// Scan observes the boundary Region's Security Lake data lakes, their log
// sources and subscribers, and the resolvable cross-service dependency metadata
// through the configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("securitylake scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceSecurityLake:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceSecurityLake
	default:
		return nil, fmt.Errorf("securitylake scanner received service_kind %q", boundary.ServiceKind)
	}

	snapshot, err := s.Client.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("snapshot Security Lake metadata: %w", err)
	}

	var envelopes []facts.Envelope
	if err := appendWarnings(&envelopes, snapshot.Warnings); err != nil {
		return nil, err
	}

	dataLakeID := primaryDataLakeID(snapshot.DataLakes)
	for _, lake := range snapshot.DataLakes {
		next, err := dataLakeEnvelopes(boundary, lake)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	for _, source := range snapshot.LogSources {
		next, err := logSourceEnvelopes(boundary, dataLakeID, source)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	for _, subscriber := range snapshot.Subscribers {
		next, err := subscriberEnvelopes(boundary, subscriber)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	return envelopes, nil
}

// primaryDataLakeID returns the resource_id of the Region's data lake so log
// sources can key membership to it. Security Lake has one data lake per Region;
// the scanner uses the first lake whose identity resolves.
func primaryDataLakeID(lakes []DataLake) string {
	for _, lake := range lakes {
		if id := dataLakeResourceID(lake); id != "" {
			return id
		}
	}
	return ""
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

func dataLakeEnvelopes(boundary awscloud.Boundary, lake DataLake) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(dataLakeObservation(boundary, lake))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	relationships := []*awscloud.RelationshipObservation{
		dataLakeS3Relationship(boundary, lake),
		dataLakeKMSRelationship(boundary, lake),
		dataLakeLakeFormationRelationship(boundary, lake),
	}
	return appendRelationships(envelopes, relationships)
}

func logSourceEnvelopes(boundary awscloud.Boundary, dataLakeID string, source LogSource) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(logSourceObservation(boundary, source))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	relationships := []*awscloud.RelationshipObservation{
		logSourceInDataLakeRelationship(boundary, dataLakeID, source),
		logSourceIAMRoleRelationship(boundary, source),
	}
	return appendRelationships(envelopes, relationships)
}

func subscriberEnvelopes(boundary awscloud.Boundary, subscriber Subscriber) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(subscriberObservation(boundary, subscriber))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	relationships := []*awscloud.RelationshipObservation{
		subscriberIAMRoleRelationship(boundary, subscriber),
		subscriberS3Relationship(boundary, subscriber),
	}
	return appendRelationships(envelopes, relationships)
}

func appendRelationships(
	envelopes []facts.Envelope,
	relationships []*awscloud.RelationshipObservation,
) ([]facts.Envelope, error) {
	for _, relationship := range relationships {
		if relationship == nil {
			continue
		}
		envelope, err := awscloud.NewRelationshipEnvelope(*relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func dataLakeObservation(boundary awscloud.Boundary, lake DataLake) awscloud.ResourceObservation {
	arn := strings.TrimSpace(lake.ARN)
	resourceID := dataLakeResourceID(lake)
	region := strings.TrimSpace(lake.Region)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeSecurityLakeDataLake,
		Name:         firstNonEmpty(region, resourceID),
		State:        strings.TrimSpace(lake.CreateStatus),
		Attributes: map[string]any{
			"region":              region,
			"s3_bucket_arn":       strings.TrimSpace(lake.S3BucketARN),
			"kms_key_id":          strings.TrimSpace(lake.KMSKeyID),
			"create_status":       strings.TrimSpace(lake.CreateStatus),
			"update_status":       strings.TrimSpace(lake.UpdateStatus),
			"expiration_days":     lake.ExpirationDays,
			"transition_count":    lake.TransitionCount,
			"replication_regions": cloneStrings(lake.ReplicationRegions),
		},
		CorrelationAnchors: []string{arn, resourceID},
		SourceRecordID:     resourceID,
	}
}

func logSourceObservation(boundary awscloud.Boundary, source LogSource) awscloud.ResourceObservation {
	resourceID := logSourceResourceID(source)
	name := strings.TrimSpace(source.SourceName)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeSecurityLakeLogSource,
		Name:         name,
		Attributes: map[string]any{
			"source_name":    name,
			"source_version": strings.TrimSpace(source.SourceVersion),
			"custom":         source.Custom,
			"account":        strings.TrimSpace(source.Account),
			"region":         strings.TrimSpace(source.Region),
		},
		CorrelationAnchors: []string{resourceID, name},
		SourceRecordID:     resourceID,
	}
}

func subscriberObservation(boundary awscloud.Boundary, subscriber Subscriber) awscloud.ResourceObservation {
	arn := strings.TrimSpace(subscriber.ARN)
	resourceID := subscriberResourceID(subscriber)
	name := strings.TrimSpace(subscriber.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeSecurityLakeSubscriber,
		Name:         name,
		State:        strings.TrimSpace(subscriber.Status),
		Attributes: map[string]any{
			"subscriber_id":     strings.TrimSpace(subscriber.ID),
			"subscriber_name":   name,
			"status":            strings.TrimSpace(subscriber.Status),
			"access_types":      cloneStrings(subscriber.AccessTypes),
			"principal_account": strings.TrimSpace(subscriber.PrincipalAccount),
			"role_arn":          strings.TrimSpace(subscriber.RoleARN),
			"s3_bucket_arn":     strings.TrimSpace(subscriber.S3BucketARN),
			"source_names":      cloneStrings(subscriber.SourceNames),
			"created_at":        timeOrNil(subscriber.CreatedAt),
			"updated_at":        timeOrNil(subscriber.UpdatedAt),
		},
		CorrelationAnchors: []string{arn, resourceID, name},
		SourceRecordID:     resourceID,
	}
}
