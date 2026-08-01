// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"encoding/json"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/cloudruntime"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// Payload decoding for the aws_cloud_runtime_drift join: turning a stored
// aws_resource or terraform_state_resource payload into a cloudruntime.ResourceRow.
// Split out of aws_cloud_runtime_drift_evidence.go when that file reached the
// repo's 500-line cap (#5859).

func awsRuntimeResourceRowFromPayload(scopeID string, payload []byte) (*cloudruntime.ResourceRow, bool) {
	var decoded struct {
		ARN          string         `json:"arn"`
		ResourceID   string         `json:"resource_id"`
		ResourceType string         `json:"resource_type"`
		Tags         map[string]any `json:"tags"`
		Attributes   map[string]any `json:"attributes"`
	}
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &decoded); err != nil {
			return nil, false
		}
	}
	arn := strings.TrimSpace(decoded.ARN)
	if arn == "" {
		return nil, false
	}
	resourceType := strings.TrimSpace(decoded.ResourceType)
	attributes, containerImages, truncated, degraded := cloudObservedValueAttributes(resourceType, decoded.Attributes)
	return &cloudruntime.ResourceRow{
		ARN:                      arn,
		ResourceID:               strings.TrimSpace(decoded.ResourceID),
		ResourceType:             resourceType,
		ScopeID:                  strings.TrimSpace(scopeID),
		Tags:                     coerceStringTags(decoded.Tags),
		Attributes:               attributes,
		ContainerImages:          containerImages,
		ContainerImagesTruncated: truncated,
		ContainerImagesDegraded:  degraded,
	}, true
}

// decodedStateARN re-reads only the "arn" attribute from a terraform_state_resource
// payload so the caller can tell a redacted join key apart from ordinary decode
// noise, without duplicating awsRuntimeStateRowFromPayload's rejection logic.
// Returns nil when the payload does not parse or carries no arn.
func decodedStateARN(payload []byte) any {
	if len(payload) == 0 {
		return nil
	}
	var decoded struct {
		Attributes map[string]any `json:"attributes"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return nil
	}
	return decoded.Attributes["arn"]
}

func awsRuntimeStateRowFromPayload(scopeID, address string, payload []byte) (*cloudruntime.ResourceRow, bool) {
	var decoded struct {
		Address    string         `json:"address"`
		Type       string         `json:"type"`
		Attributes map[string]any `json:"attributes"`
	}
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &decoded); err != nil {
			return nil, false
		}
	}
	if decoded.Address != "" {
		address = decoded.Address
	}
	address = strings.TrimSpace(address)
	// "arn" is the join key, not a value, so a redacted one has to be REJECTED
	// rather than carried. LoadPackagedSchemaResolver returns (nil, nil) when no
	// provider-schema bundle parses, and schemaTrust answers SchemaUnknown for
	// every attribute against a nil resolver, so the state parser fail-closed-
	// redacts "arn" along with everything else. coerceJSONString renders that
	// marker map through fmt.Sprint into a NON-EMPTY garbage string, which
	// satisfies the emptiness guard below and then keys stateByARN. It matches no
	// observed ARN, so the declared side does not lose a comparison -- it leaves
	// the join, and every cloud resource under that bundle reclassifies as
	// orphaned_cloud_resource. Dropping the row instead surfaces as missing
	// declared evidence, which is what the empty-ARN guard already means (#5870).
	if redact.IsRedactedValue(decoded.Attributes["arn"]) {
		return nil, false
	}
	arn := strings.TrimSpace(coerceJSONString(decoded.Attributes["arn"]))
	if address == "" || arn == "" {
		return nil, false
	}
	resourceType := strings.TrimSpace(decoded.Type)
	attributes, containerImages, truncated, degraded := stateDeclaredValueAttributes(resourceType, decoded.Attributes)
	return &cloudruntime.ResourceRow{
		ARN:                      arn,
		Address:                  address,
		ResourceType:             resourceType,
		ScopeID:                  strings.TrimSpace(scopeID),
		Attributes:               attributes,
		ContainerImages:          containerImages,
		ContainerImagesTruncated: truncated,
		ContainerImagesDegraded:  degraded,
	}, true
}

func coerceStringTags(tags map[string]any) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	out := make(map[string]string, len(tags))
	for key, value := range tags {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = coerceJSONString(value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
