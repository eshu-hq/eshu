package awscloud

import (
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
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
	payload := map[string]any{
		"account_id":            observation.Boundary.AccountID,
		"region":                observation.Boundary.Region,
		"service_kind":          observation.Boundary.ServiceKind,
		"collector_instance_id": observation.Boundary.CollectorInstanceID,
		"arn":                   arn,
		"resource_id":           resourceID,
		"resource_type":         resourceType,
		"name":                  strings.TrimSpace(observation.Name),
		"state":                 strings.TrimSpace(observation.State),
		"tags":                  cloneStringMap(observation.Tags),
		"attributes":            cloneAnyMap(observation.Attributes),
		"correlation_anchors":   anchors,
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
	payload := map[string]any{
		"account_id":            observation.Boundary.AccountID,
		"region":                observation.Boundary.Region,
		"service_kind":          observation.Boundary.ServiceKind,
		"collector_instance_id": observation.Boundary.CollectorInstanceID,
		"relationship_type":     relationshipType,
		"source_resource_id":    sourceID,
		"source_arn":            strings.TrimSpace(observation.SourceARN),
		"target_resource_id":    targetID,
		"target_arn":            strings.TrimSpace(observation.TargetARN),
		"target_type":           strings.TrimSpace(observation.TargetType),
		"attributes":            cloneAnyMap(observation.Attributes),
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
	payload := map[string]any{
		"account_id":            observation.Boundary.AccountID,
		"region":                observation.Boundary.Region,
		"service_kind":          observation.Boundary.ServiceKind,
		"collector_instance_id": observation.Boundary.CollectorInstanceID,
		"warning_kind":          warningKind,
		"error_class":           strings.TrimSpace(observation.ErrorClass),
		"message":               strings.TrimSpace(observation.Message),
		"attributes":            cloneAnyMap(observation.Attributes),
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
