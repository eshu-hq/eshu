// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicecatalogappregistry

import (
	"strings"
	"time"
)

// applicationResourceID returns the resource_id the application node publishes.
// It prefers the application ARN (always present from the AppRegistry API) and
// falls back to the application id, so the application's own edges key on the
// same value the node publishes.
func applicationResourceID(application Application) string {
	return firstNonEmpty(application.ARN, application.ID)
}

// attributeGroupResourceID returns the resource_id the attribute-group node
// publishes. It prefers the group ARN and falls back to the group id, so
// application-to-attribute-group edges key the group by the value its node
// publishes.
func attributeGroupResourceID(group AttributeGroup) string {
	return firstNonEmpty(group.ARN, group.ID)
}

// cfnStackAssociationType is the AppRegistry associated-resource type whose ARN
// is a CloudFormation stack ARN. Other associated-resource types (for example
// RESOURCE_TAG_VALUE) are not CloudFormation stacks, so the stack edge is gated
// on this exact type.
const cfnStackAssociationType = "CFN_STACK"

// isCloudFormationStackARN reports whether value is a CloudFormation stack ARN.
// AppRegistry reports the stack ARN for CFN_STACK associations, which matches
// the resource_id the cloudformation scanner publishes for a stack node.
func isCloudFormationStackARN(value string) bool {
	value = strings.TrimSpace(value)
	return strings.HasPrefix(value, "arn:") && strings.Contains(value, ":cloudformation:") &&
		strings.Contains(value, ":stack/")
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
