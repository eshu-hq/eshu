// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package verifiedpermissions

import (
	"strings"
	"time"
)

// policyStoreResourceID returns the resource_id the policy store node
// publishes. It prefers the policy store ARN (always present from the API) and
// falls back to the policy store id, so policy-in-store and
// identity-source-in-store edges can key the store by the same value the node
// publishes.
func policyStoreResourceID(store PolicyStore) string {
	return firstNonEmpty(store.ARN, store.ID)
}

// policyResourceID returns the resource_id a policy node publishes. Policies
// have no API ARN, so the scanner keys them by the qualified
// "<policy-store-id>/<policy-id>" so each policy id stays unique across stores
// and the policy-in-store edge sources on the same value the node publishes.
func policyResourceID(policy Policy) string {
	policyID := strings.TrimSpace(policy.ID)
	storeID := strings.TrimSpace(policy.PolicyStoreID)
	switch {
	case storeID != "" && policyID != "":
		return storeID + "/" + policyID
	default:
		return policyID
	}
}

// identitySourceResourceID returns the resource_id an identity source node
// publishes. Identity sources have no API ARN, so the scanner keys them by the
// qualified "<policy-store-id>/<identity-source-id>" so the id stays unique
// across stores.
func identitySourceResourceID(source IdentitySource) string {
	sourceID := strings.TrimSpace(source.ID)
	storeID := strings.TrimSpace(source.PolicyStoreID)
	switch {
	case storeID != "" && sourceID != "":
		return storeID + "/" + sourceID
	default:
		return sourceID
	}
}

// cognitoUserPoolID extracts the bare Cognito user pool id from a user pool
// ARN of the shape
// arn:<partition>:cognito-idp:<region>:<account>:userpool/<user-pool-id>. The
// Cognito scanner publishes a user pool node's resource_id as the bare user
// pool id, so the identity-source-to-user-pool edge must key the target on this
// extracted id rather than the ARN. It returns "" when value is not a user pool
// ARN, so a malformed reference skips the edge instead of dangling it.
func cognitoUserPoolID(arn string) string {
	arn = strings.TrimSpace(arn)
	if arn == "" {
		return ""
	}
	marker := ":userpool/"
	idx := strings.LastIndex(arn, marker)
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(arn[idx+len(marker):])
}

// isARN reports whether value carries the canonical AWS ARN prefix.
func isARN(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), "arn:")
}

// firstNonEmpty returns the first trimmed non-empty value, or "" when none.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// timeOrNil returns the UTC time when value is set, or nil for the zero time so
// the attribute payload omits an unknown timestamp instead of emitting an epoch.
func timeOrNil(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

// cloneStringMap returns a trimmed-key copy of input, or nil when it is empty or
// every key trims to empty, keeping omitempty-style payload behavior consistent.
func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		output[trimmed] = value
	}
	if len(output) == 0 {
		return nil
	}
	return output
}
