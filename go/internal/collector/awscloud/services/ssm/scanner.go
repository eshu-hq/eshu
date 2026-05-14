package ssm

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits SSM Parameter Store metadata facts for one claimed account and
// region. It never reads parameter values, parameter history, raw policy JSON,
// or mutates SSM resources.
type Scanner struct {
	Client Client
}

// Scan observes Parameter Store metadata and direct KMS dependency metadata
// through the configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("ssm scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "":
		boundary.ServiceKind = awscloud.ServiceSSM
	case awscloud.ServiceSSM:
	default:
		return nil, fmt.Errorf("ssm scanner received service_kind %q", boundary.ServiceKind)
	}

	parameters, err := s.Client.ListParameters(ctx)
	if err != nil {
		return nil, fmt.Errorf("list SSM parameters: %w", err)
	}
	var envelopes []facts.Envelope
	for _, parameter := range parameters {
		parameterEnvelopes, err := parameterEnvelopes(boundary, parameter)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, parameterEnvelopes...)
	}
	return envelopes, nil
}

func parameterEnvelopes(boundary awscloud.Boundary, parameter Parameter) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(parameterObservation(boundary, parameter))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if relationship := kmsRelationship(boundary, parameter); relationship != nil {
		envelope, err := awscloud.NewRelationshipEnvelope(*relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func parameterObservation(boundary awscloud.Boundary, parameter Parameter) awscloud.ResourceObservation {
	parameterARN := strings.TrimSpace(parameter.ARN)
	name := strings.TrimSpace(parameter.Name)
	resourceID := parameterResourceID(parameter)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          parameterARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeSSMParameter,
		Name:         name,
		Tags:         cloneStringMap(parameter.Tags),
		Attributes: map[string]any{
			"type":                    strings.TrimSpace(parameter.Type),
			"tier":                    strings.TrimSpace(parameter.Tier),
			"data_type":               strings.TrimSpace(parameter.DataType),
			"key_id":                  strings.TrimSpace(parameter.KeyID),
			"last_modified_at":        timeOrNil(parameter.LastModifiedAt),
			"description_present":     parameter.DescriptionPresent,
			"allowed_pattern_present": parameter.AllowedPatternPresent,
			"policies":                policyAttributes(parameter.Policies),
		},
		CorrelationAnchors: []string{parameterARN, name},
		SourceRecordID:     resourceID,
	}
}

func policyAttributes(policies []PolicyMetadata) []map[string]string {
	if len(policies) == 0 {
		return nil
	}
	output := make([]map[string]string, 0, len(policies))
	for _, policy := range policies {
		entry := map[string]string{
			"type":   strings.TrimSpace(policy.Type),
			"status": strings.TrimSpace(policy.Status),
		}
		if entry["type"] == "" && entry["status"] == "" {
			continue
		}
		output = append(output, entry)
	}
	if len(output) == 0 {
		return nil
	}
	return output
}
