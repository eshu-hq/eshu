// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicecatalogappregistry

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS Service Catalog AppRegistry metadata-only facts for one
// claimed account and region. It reports applications and attribute groups plus
// the application-to-attribute-group and application-to-CloudFormation-stack
// relationships. It never reads or persists attribute-group content bodies or
// associated-resource tag values, and never mutates AppRegistry state.
type Scanner struct {
	// Client is the metadata-only AppRegistry snapshot source.
	Client Client
}

// Scan observes AppRegistry applications, attribute groups, and the application
// associations (attribute groups and CloudFormation stacks) through the
// configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("servicecatalogappregistry scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceServiceCatalogAppRegistry:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceServiceCatalogAppRegistry
	default:
		return nil, fmt.Errorf(
			"servicecatalogappregistry scanner received service_kind %q",
			boundary.ServiceKind,
		)
	}

	snapshot, err := s.Client.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("list AppRegistry applications: %w", err)
	}
	var envelopes []facts.Envelope
	if err := appendWarnings(&envelopes, snapshot.Warnings); err != nil {
		return nil, err
	}
	for _, group := range snapshot.AttributeGroups {
		envelope, err := awscloud.NewResourceEnvelope(attributeGroupObservation(boundary, group))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	for _, application := range snapshot.Applications {
		next, err := applicationEnvelopes(boundary, application)
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

func applicationEnvelopes(boundary awscloud.Boundary, application Application) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(applicationObservation(boundary, application))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	relationships := applicationAttributeGroupRelationships(boundary, application)
	relationships = append(relationships, applicationStackRelationships(boundary, application)...)
	for _, relationship := range relationships {
		envelope, err := awscloud.NewRelationshipEnvelope(relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func applicationObservation(boundary awscloud.Boundary, application Application) awscloud.ResourceObservation {
	arn := strings.TrimSpace(application.ARN)
	name := strings.TrimSpace(application.Name)
	resourceID := applicationResourceID(application)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeServiceCatalogAppRegistryApplication,
		Name:         name,
		Tags:         cloneStringMap(application.Tags),
		Attributes: map[string]any{
			"application_id":            strings.TrimSpace(application.ID),
			"description":               strings.TrimSpace(application.Description),
			"attribute_group_count":     len(application.AttributeGroupARNs),
			"associated_resource_count": len(application.AssociatedResources),
			"creation_time":             timeOrNil(application.CreationTime),
			"last_update_time":          timeOrNil(application.LastUpdateTime),
		},
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     resourceID,
	}
}

func attributeGroupObservation(boundary awscloud.Boundary, group AttributeGroup) awscloud.ResourceObservation {
	arn := strings.TrimSpace(group.ARN)
	name := strings.TrimSpace(group.Name)
	resourceID := attributeGroupResourceID(group)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeServiceCatalogAppRegistryAttributeGroup,
		Name:         name,
		Tags:         cloneStringMap(group.Tags),
		Attributes: map[string]any{
			"attribute_group_id": strings.TrimSpace(group.ID),
			"description":        strings.TrimSpace(group.Description),
			"creation_time":      timeOrNil(group.CreationTime),
			"last_update_time":   timeOrNil(group.LastUpdateTime),
		},
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     resourceID,
	}
}
