// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package opensearchserverless

import (
	"strings"
	"time"
)

// collectionResourceID returns the resource_id the collection node publishes. It
// prefers the collection ARN (always present from BatchGetCollection) and falls
// back to the collection id then name, so a collection's own edges are sourced on
// the same id the collection node publishes.
func collectionResourceID(collection Collection) string {
	return firstNonEmpty(collection.ARN, collection.ID, collection.Name)
}

// vpcEndpointResourceID returns the resource_id the VPC endpoint node publishes.
// OpenSearch Serverless managed endpoints have no API ARN, so the bare vpce-… id
// (falling back to the name) is the published identity.
func vpcEndpointResourceID(endpoint VPCEndpoint) string {
	return firstNonEmpty(endpoint.ID, endpoint.Name)
}

// securityPolicyResourceID returns the resource_id a security policy node
// publishes. Policy names are unique per type, so the type-qualified name keys
// the resource stably across encryption and network policies that may share a
// name.
func securityPolicyResourceID(policy SecurityPolicy) string {
	name := strings.TrimSpace(policy.Name)
	policyType := strings.TrimSpace(policy.Type)
	switch {
	case name == "":
		return ""
	case policyType != "":
		return policyType + "/" + name
	default:
		return name
	}
}

// matchEncryptionKey returns the customer-managed KMS key ARN and source policy
// name assigned to collectionName by the encryption policy bindings, applying
// AWS's documented "most specific rule wins" precedence: an exact pattern beats a
// prefix pattern, and a longer prefix beats a shorter one. It returns empty
// strings when no binding matches or the matching binding uses an AWS-owned key.
func matchEncryptionKey(bindings []EncryptionKeyBinding, collectionName string) (keyARN, policyName string) {
	name := strings.TrimSpace(collectionName)
	if name == "" {
		return "", ""
	}
	bestScore := -1
	for _, binding := range bindings {
		bindingKeyARN := strings.TrimSpace(binding.KMSKeyARN)
		if bindingKeyARN == "" {
			// AWS-owned-key policy: matches the name but assigns no edge target.
			continue
		}
		for _, pattern := range binding.CollectionPatterns {
			score, ok := patternMatchScore(pattern, name)
			if !ok || score <= bestScore {
				continue
			}
			bestScore = score
			keyARN = bindingKeyARN
			policyName = strings.TrimSpace(binding.PolicyName)
		}
	}
	if bestScore < 0 {
		return "", ""
	}
	return keyARN, policyName
}

// patternMatchScore reports whether pattern matches name and a specificity score
// for precedence. An exact match scores by name length plus a large exact-match
// bonus so it always beats any prefix match; a prefix match (pattern ending in
// "*") scores by the literal prefix length. It returns ok=false when the pattern
// does not match name.
func patternMatchScore(pattern, name string) (score int, ok bool) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return 0, false
	}
	const exactBonus = 1 << 20
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		if strings.HasPrefix(name, prefix) {
			return len(prefix), true
		}
		return 0, false
	}
	if pattern == name {
		return exactBonus + len(name), true
	}
	return 0, false
}

// CollectionPatternFromResource strips the leading "collection/" prefix from an
// encryption-policy resource entry, returning the bare collection name or prefix
// pattern. It returns "" for entries that are not collection resources, so only
// collection-scoped patterns drive collection-to-KMS edges. It is exported so the
// SDK adapter can project the encryption policy body into collection patterns
// without re-deriving the resource convention.
func CollectionPatternFromResource(resource string) string {
	resource = strings.TrimSpace(resource)
	const prefix = "collection/"
	if !strings.HasPrefix(resource, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(resource, prefix))
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

// cloneStrings returns a trimmed copy of input with empty entries dropped, or nil
// when nothing survives.
func cloneStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, 0, len(input))
	for _, value := range input {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			output = append(output, trimmed)
		}
	}
	if len(output) == 0 {
		return nil
	}
	return output
}
