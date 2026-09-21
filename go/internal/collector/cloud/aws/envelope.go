// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

import (
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
)

// NewResourceEnvelope builds the durable aws_resource fact for one AWS
// resource reported by a service API.
func NewResourceEnvelope(observation ResourceObservation) (facts.Envelope, error) {
	if err := validateBoundary(observation.Boundary); err != nil {
		return facts.Envelope{}, err
	}
	arn := strings.TrimSpace(observation.ARN)
	resourceID := strings.TrimSpace(observation.ResourceID)
	if arn == "" && resourceID == "" {
		return facts.Envelope{}, fmt.Errorf("aws resource observation requires arn or resource_id")
	}
	if resourceID == "" {
		resourceID = arn
	}
	resourceType := strings.TrimSpace(observation.ResourceType)
	if resourceType == "" {
		return facts.Envelope{}, fmt.Errorf("aws resource observation requires resource_type")
	}
	stableKey := facts.StableID(facts.AWSResourceFactKind, map[string]any{
		"account_id":    observation.Boundary.AccountID,
		"region":        observation.Boundary.Region,
		"resource_id":   resourceID,
		"resource_type": resourceType,
	})
	anchors := normalizedAnchors(observation.CorrelationAnchors, arn, resourceID)
	tags := cloneStringMap(observation.Tags)
	payload, err := factschema.EncodeAWSResource(awsv1.Resource{
		AccountID:          observation.Boundary.AccountID,
		Region:             observation.Boundary.Region,
		ServiceKind:        boundaryValue(observation.Boundary.ServiceKind),
		ARN:                stringValuePtr(arn),
		ResourceID:         resourceID,
		ResourceType:       resourceType,
		Name:               stringValuePtr(strings.TrimSpace(observation.Name)),
		State:              stringValuePtr(strings.TrimSpace(observation.State)),
		Tags:               &tags,
		Attributes:         awsPayloadAttributes(observation.Boundary, observation.Attributes),
		CorrelationAnchors: anchors,
	})
	if err != nil {
		return facts.Envelope{}, fmt.Errorf("encode aws_resource payload: %w", err)
	}
	return newEnvelope(
		observation.Boundary,
		facts.AWSResourceFactKind,
		facts.AWSResourceSchemaVersion,
		stableKey,
		sourceRecordID(observation.SourceRecordID, resourceID),
		observation.SourceURI,
		payload,
	), nil
}

// NewRelationshipEnvelope builds the durable aws_relationship fact for one AWS
// relationship reported by a service API.
func NewRelationshipEnvelope(observation RelationshipObservation) (facts.Envelope, error) {
	if err := validateBoundary(observation.Boundary); err != nil {
		return facts.Envelope{}, err
	}
	relationshipType := strings.TrimSpace(observation.RelationshipType)
	sourceID := strings.TrimSpace(observation.SourceResourceID)
	targetID := strings.TrimSpace(observation.TargetResourceID)
	if relationshipType == "" {
		return facts.Envelope{}, fmt.Errorf("aws relationship observation requires relationship_type")
	}
	if sourceID == "" {
		sourceID = strings.TrimSpace(observation.SourceARN)
	}
	if targetID == "" {
		targetID = strings.TrimSpace(observation.TargetARN)
	}
	if sourceID == "" || targetID == "" {
		return facts.Envelope{}, fmt.Errorf("aws relationship observation requires source and target identity")
	}
	stableKey := facts.StableID(facts.AWSRelationshipFactKind, map[string]any{
		"account_id":         observation.Boundary.AccountID,
		"region":             observation.Boundary.Region,
		"relationship_type":  relationshipType,
		"source_resource_id": sourceID,
		"target_resource_id": targetID,
	})
	payload, err := factschema.EncodeAWSRelationship(awsv1.Relationship{
		AccountID:        observation.Boundary.AccountID,
		Region:           observation.Boundary.Region,
		RelationshipType: relationshipType,
		SourceResourceID: sourceID,
		SourceARN:        stringValuePtr(strings.TrimSpace(observation.SourceARN)),
		TargetResourceID: targetID,
		TargetARN:        stringValuePtr(strings.TrimSpace(observation.TargetARN)),
		TargetType:       stringValuePtr(strings.TrimSpace(observation.TargetType)),
		Attributes:       awsPayloadAttributes(observation.Boundary, observation.Attributes),
	})
	if err != nil {
		return facts.Envelope{}, fmt.Errorf("encode aws_relationship payload: %w", err)
	}
	return newEnvelope(
		observation.Boundary,
		facts.AWSRelationshipFactKind,
		facts.AWSRelationshipSchemaVersion,
		stableKey,
		sourceRecordID(observation.SourceRecordID, sourceID+"->"+targetID),
		observation.SourceURI,
		payload,
	), nil
}

// NewImageReferenceEnvelope builds the durable aws_image_reference fact for one
// ECR repository image digest and tag reference.
func NewImageReferenceEnvelope(observation ImageReferenceObservation) (facts.Envelope, error) {
	if err := validateBoundary(observation.Boundary); err != nil {
		return facts.Envelope{}, err
	}
	repositoryName := strings.TrimSpace(observation.RepositoryName)
	imageDigest := strings.TrimSpace(observation.ImageDigest)
	if repositoryName == "" {
		return facts.Envelope{}, fmt.Errorf("aws image reference observation requires repository_name")
	}
	if imageDigest == "" {
		return facts.Envelope{}, fmt.Errorf("aws image reference observation requires image_digest")
	}
	tag := strings.TrimSpace(observation.Tag)
	manifestDigest := strings.TrimSpace(observation.ManifestDigest)
	if manifestDigest == "" {
		manifestDigest = imageDigest
	}
	// registry_id is part of the identity, not just a carried attribute: two
	// observations can share account_id/region/repository_name/image_digest/tag
	// while naming DIFFERENT registry accounts — a cross-account ECR pull,
	// where the pulling boundary's account_id differs from the image's actual
	// owning registry. Omitting registry_id from the key let two distinct
	// cross-account images collide onto the same FactID, silently dropping
	// one (codex #5451 P2 finding: the ECS scanner parses RegistryID from the
	// image host specifically to support this case, so the key must key on it
	// too or that parsing is pointless).
	stableKey := facts.StableID(facts.AWSImageReferenceFactKind, map[string]any{
		"account_id":      observation.Boundary.AccountID,
		"image_digest":    imageDigest,
		"region":          observation.Boundary.Region,
		"registry_id":     strings.TrimSpace(observation.RegistryID),
		"repository_name": repositoryName,
		"tag":             tag,
	})
	payload, err := factschema.EncodeAWSImageReference(awsv1.ImageReference{
		AccountID:           observation.Boundary.AccountID,
		Region:              observation.Boundary.Region,
		ServiceKind:         boundaryValue(observation.Boundary.ServiceKind),
		CollectorInstanceID: boundaryValue(observation.Boundary.CollectorInstanceID),
		RepositoryARN:       stringValuePtr(strings.TrimSpace(observation.RepositoryARN)),
		RepositoryName:      repositoryName,
		RegistryID:          stringValuePtr(strings.TrimSpace(observation.RegistryID)),
		ImageDigest:         imageDigest,
		ManifestDigest:      manifestDigest,
		Tag:                 stringValuePtr(tag),
		PushedAt:            timeValuePtr(observation.PushedAt),
		ImageSizeInBytes:    int64ValuePtr(observation.ImageSizeInBytes),
		ManifestMediaType:   stringValuePtr(strings.TrimSpace(observation.ManifestMediaType)),
		ArtifactMediaType:   stringValuePtr(strings.TrimSpace(observation.ArtifactMediaType)),
		CorrelationAnchors:  imageReferenceAnchors(repositoryName, imageDigest, manifestDigest, tag),
	})
	if err != nil {
		return facts.Envelope{}, fmt.Errorf("encode aws_image_reference payload: %w", err)
	}
	return newEnvelope(
		observation.Boundary,
		facts.AWSImageReferenceFactKind,
		facts.AWSImageReferenceSchemaVersion,
		stableKey,
		sourceRecordID(observation.SourceRecordID, repositoryName+"@"+imageDigest+"#"+tag),
		observation.SourceURI,
		payload,
	), nil
}

// NewWarningEnvelope builds the durable aws_warning fact for a non-fatal AWS
// scan condition.
func NewWarningEnvelope(observation WarningObservation) (facts.Envelope, error) {
	if err := validateBoundary(observation.Boundary); err != nil {
		return facts.Envelope{}, err
	}
	warningKind := strings.TrimSpace(observation.WarningKind)
	if warningKind == "" {
		return facts.Envelope{}, fmt.Errorf("aws warning observation requires warning_kind")
	}
	stableKey := facts.StableID(facts.AWSWarningFactKind, map[string]any{
		"account_id":   observation.Boundary.AccountID,
		"error_class":  strings.TrimSpace(observation.ErrorClass),
		"generation":   observation.Boundary.GenerationID,
		"region":       observation.Boundary.Region,
		"service_kind": observation.Boundary.ServiceKind,
		"warning_kind": warningKind,
	})
	payload, err := factschema.EncodeAWSWarning(awsv1.Warning{
		AccountID:           observation.Boundary.AccountID,
		Region:              observation.Boundary.Region,
		ServiceKind:         boundaryValue(observation.Boundary.ServiceKind),
		CollectorInstanceID: boundaryValue(observation.Boundary.CollectorInstanceID),
		WarningKind:         warningKind,
		ErrorClass:          stringValuePtr(strings.TrimSpace(observation.ErrorClass)),
		Message:             stringValuePtr(strings.TrimSpace(observation.Message)),
		Attributes:          cloneAnyMap(observation.Attributes),
	})
	if err != nil {
		return facts.Envelope{}, fmt.Errorf("encode aws_warning payload: %w", err)
	}
	return newEnvelope(
		observation.Boundary,
		facts.AWSWarningFactKind,
		facts.AWSWarningSchemaVersion,
		stableKey,
		sourceRecordID(observation.SourceRecordID, warningKind),
		observation.SourceURI,
		payload,
	), nil
}

func newEnvelope(
	boundary Boundary,
	factKind string,
	schemaVersion string,
	stableKey string,
	sourceRecordID string,
	sourceURI string,
	payload map[string]any,
) facts.Envelope {
	observedAt := boundary.ObservedAt.UTC()
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}
	return facts.Envelope{
		FactID:           awsFactID(factKind, stableKey, boundary.ScopeID, boundary.GenerationID),
		ScopeID:          boundary.ScopeID,
		GenerationID:     boundary.GenerationID,
		FactKind:         factKind,
		StableFactKey:    stableKey,
		SchemaVersion:    schemaVersion,
		CollectorKind:    CollectorKind,
		FencingToken:     boundary.FencingToken,
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       observedAt,
		Payload:          payload,
		SourceRef: facts.Ref{
			SourceSystem:   CollectorKind,
			ScopeID:        boundary.ScopeID,
			GenerationID:   boundary.GenerationID,
			FactKey:        stableKey,
			SourceURI:      strings.TrimSpace(sourceURI),
			SourceRecordID: sourceRecordID,
		},
	}
}

func awsFactID(factKind, stableFactKey, scopeID, generationID string) string {
	return facts.StableID("AWSFact", map[string]any{
		"fact_kind":       factKind,
		"generation_id":   generationID,
		"scope_id":        scopeID,
		"stable_fact_key": stableFactKey,
	})
}

func validateBoundary(boundary Boundary) error {
	switch {
	case strings.TrimSpace(boundary.AccountID) == "":
		return fmt.Errorf("aws observation requires account_id")
	case strings.TrimSpace(boundary.Region) == "":
		return fmt.Errorf("aws observation requires region")
	case strings.TrimSpace(boundary.ServiceKind) == "":
		return fmt.Errorf("aws observation requires service_kind")
	case strings.TrimSpace(boundary.ScopeID) == "":
		return fmt.Errorf("aws observation requires scope_id")
	case strings.TrimSpace(boundary.GenerationID) == "":
		return fmt.Errorf("aws observation requires generation_id")
	case strings.TrimSpace(boundary.CollectorInstanceID) == "":
		return fmt.Errorf("aws observation requires collector_instance_id")
	case boundary.FencingToken <= 0:
		return fmt.Errorf("aws observation fencing_token must be positive")
	default:
		return nil
	}
}

func normalizedAnchors(values []string, fallback ...string) []string {
	seen := make(map[string]struct{}, len(values)+len(fallback))
	anchors := make([]string, 0, len(values)+len(fallback))
	for _, value := range append(values, fallback...) {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		anchors = append(anchors, trimmed)
	}
	return anchors
}

func imageReferenceAnchors(repositoryName, imageDigest, manifestDigest, tag string) []string {
	anchors := []string{imageDigest, manifestDigest, repositoryName + "@" + imageDigest}
	if strings.TrimSpace(tag) != "" {
		anchors = append(anchors, repositoryName+":"+tag)
	}
	return normalizedAnchors(nil, anchors...)
}

func sourceRecordID(candidate, fallback string) string {
	candidate = strings.TrimSpace(candidate)
	if candidate != "" {
		return candidate
	}
	return strings.TrimSpace(fallback)
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func cloneAnyMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}
