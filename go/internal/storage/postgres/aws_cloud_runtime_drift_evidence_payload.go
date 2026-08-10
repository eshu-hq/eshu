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

// Failure classes for a terraform_state_resource row awsRuntimeStateRowFromPayload
// refused. They are log labels, not finding kinds: the correlation outcome is the
// same for both (see the join-key comment in awsRuntimeStateRowFromPayload). The
// split exists so an operator can tell which one they are looking at.
const (
	// stateResourceDecodeFailure is ordinary malformed-payload noise: bad JSON,
	// a missing address, or a missing arn.
	stateResourceDecodeFailure = "state_resource_payload_decode"
	// stateResourceARNRedacted means the join key itself came back redacted, so
	// this deployment has no usable provider-schema bundle and EVERY declared
	// row under it is dropping out of the join. The action is to fix the bundle,
	// not to investigate one payload (#5859, #5870).
	stateResourceARNRedacted = "state_resource_arn_redacted"
)

// awsRuntimeStateRowFromPayload decodes one terraform_state_resource payload
// into a cloudruntime.ResourceRow. On failure it returns (nil, false,
// failureClass) so the caller can log why without re-unmarshaling the payload
// it already decoded: in the broken-provider-schema-bundle case (#5859,
// #5870) EVERY state row takes this branch, so a second json.Unmarshal here
// would be the hot path of the degraded run, not a rare fallback. failureClass
// is meaningless when ok is true.
func awsRuntimeStateRowFromPayload(scopeID, address string, payload []byte) (row *cloudruntime.ResourceRow, ok bool, failureClass string) {
	var decoded struct {
		Address    string         `json:"address"`
		Type       string         `json:"type"`
		Attributes map[string]any `json:"attributes"`
	}
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &decoded); err != nil {
			return nil, false, stateResourceDecodeFailure
		}
	}
	if decoded.Address != "" {
		address = decoded.Address
	}
	address = strings.TrimSpace(address)
	// "arn" is the join key, not a value, so a redacted one is rejected rather
	// than carried.
	//
	// Until #5870, LoadPackagedSchemaResolver returned (nil, nil) when no
	// provider-schema bundle parsed, and schemaTrust answers SchemaUnknown for
	// every attribute against a nil resolver, so the state parser
	// fail-closed-redacted "arn" along with everything else. That constructor
	// now fails the collector's startup instead, so it is no longer a source of
	// a nil resolver.
	//
	// This guard stays as defense in depth: SchemaUnknown is the answer for any
	// pair a resolver does not carry, not only for a nil one, so a redacted
	// "arn" remains reachable. coerceJSONString renders that marker map through
	// fmt.Sprint into a NON-EMPTY garbage string, which satisfies the emptiness
	// guard below and then keys stateByARN.
	//
	// Be precise about what this does and does not change. The correlation
	// OUTCOME is the same either way: the caller iterates observed ARNs and
	// looks up stateByARN[arn], so a garbage key is simply never read, and the
	// resource classifies orphaned_cloud_resource whether the row was stored
	// under a key nothing matches or never stored at all. What rejecting buys
	// is that the failure becomes VISIBLE -- the caller can tell a redacted
	// join key apart from ordinary decode noise and log
	// state_resource_arn_redacted, which points an operator at the schema
	// bundle instead of at a generic malformed-payload warning. Whether the
	// collector should fail-closed-redact an identity anchor at all is the
	// upstream policy question, and stays open on #5870.
	if redact.IsRedactedValue(decoded.Attributes["arn"]) {
		return nil, false, stateResourceARNRedacted
	}
	arn := strings.TrimSpace(coerceJSONString(decoded.Attributes["arn"]))
	if address == "" || arn == "" {
		return nil, false, stateResourceDecodeFailure
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
	}, true, ""
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
