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
	row := &cloudruntime.ResourceRow{
		ARN:          arn,
		ResourceID:   strings.TrimSpace(decoded.ResourceID),
		ResourceType: resourceType,
		ScopeID:      strings.TrimSpace(scopeID),
		Tags:         coerceStringTags(decoded.Tags),
	}
	cloudObservedValueAttributes(resourceType, decoded.Attributes).applyTo(row)
	return row, true
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
	// This guard is NOT reachable through the only production caller, and the
	// earlier version of this comment was wrong to imply otherwise (#6017
	// review).
	//
	// listActiveStateResourcesForAWSARNsQuery inner-joins
	// aws_arn.arn = fact.payload->'attributes'->>'arn' against ARNs already
	// loaded from the AWS generation. A redaction marker renders as JSON text
	// that cannot equal a real ARN, so a state row with a redacted "arn" is
	// dropped by that join and never reaches this decode. It never becomes a
	// stateByARN key, and state_resource_arn_redacted cannot fire from that
	// path.
	//
	// So the production consequence of a redacted "arn" is quieter than a
	// garbage key: the state row is simply never loaded, the AWS resource finds
	// no state to compare against, and it classifies orphaned_cloud_resource
	// with nothing anywhere naming redaction as the cause. That is precisely
	// why #5870 fails the collector at startup instead — by the time a redacted
	// join key reaches SQL there is no longer anywhere to catch it.
	//
	// The check is kept as defense in depth for a future caller that loads
	// state rows WITHOUT pre-filtering on a real ARN; such a caller would reach
	// this decode, and rejecting is right there because coerceJSONString renders
	// the marker map through fmt.Sprint into a NON-EMPTY string that would
	// satisfy the emptiness guard below. Whether the collector should
	// fail-closed-redact an identity anchor at all is the upstream policy
	// question, and stays open on #5870.
	if redact.IsRedactedValue(decoded.Attributes["arn"]) {
		return nil, false, stateResourceARNRedacted
	}
	arn := strings.TrimSpace(coerceJSONString(decoded.Attributes["arn"]))
	if address == "" || arn == "" {
		return nil, false, stateResourceDecodeFailure
	}
	resourceType := strings.TrimSpace(decoded.Type)
	stateRow := &cloudruntime.ResourceRow{
		ARN:          arn,
		Address:      address,
		ResourceType: resourceType,
		ScopeID:      strings.TrimSpace(scopeID),
	}
	stateDeclaredValueAttributes(resourceType, decoded.Attributes).applyTo(stateRow)
	return stateRow, true, ""
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
