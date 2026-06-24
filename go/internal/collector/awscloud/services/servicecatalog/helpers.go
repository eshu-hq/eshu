// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicecatalog

import (
	"strings"
	"time"
)

// firstNonEmpty returns the first trimmed, non-empty value in values, or the
// empty string when every value is blank. The scanner sources a resource's own
// outgoing edges on the same identifier it publishes as the node resource_id
// (firstNonEmpty(arn, id)), so the edge resolves to the node instead of
// dangling.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// arnService returns the service segment of an AWS ARN, the third
// colon-delimited field (arn:partition:service:...). It returns the empty
// string when value is not an ARN with a populated service segment. The match
// is exact on the parsed field, never a substring of the whole ARN, so an
// identifier that merely contains ":iam:" or ":cloudformation:" inside an
// unrelated segment cannot be misclassified.
func arnService(value string) string {
	trimmed := strings.TrimSpace(value)
	if !strings.HasPrefix(trimmed, "arn:") {
		return ""
	}
	parts := strings.SplitN(trimmed, ":", 4)
	if len(parts) < 3 {
		return ""
	}
	return strings.TrimSpace(parts[2])
}

// arnResource returns the resource portion of an AWS ARN, everything after the
// fifth colon-delimited field (arn:partition:service:region:account:resource).
// It returns the empty string when value does not have a resource segment.
func arnResource(value string) string {
	trimmed := strings.TrimSpace(value)
	if !strings.HasPrefix(trimmed, "arn:") {
		return ""
	}
	parts := strings.SplitN(trimmed, ":", 6)
	if len(parts) < 6 {
		return ""
	}
	return strings.TrimSpace(parts[5])
}

// isIAMRoleARN reports whether value is a fully defined IAM role ARN: the
// service segment is exactly iam and the resource segment names a role
// (role/... or role with a path). It rejects IAM users, groups, and the
// wildcard IAM_PATTERN principals Service Catalog allows, since those name no
// concrete IAM role node the IAM scanner publishes.
func isIAMRoleARN(value string) bool {
	if arnService(value) != "iam" {
		return false
	}
	resource := arnResource(value)
	if strings.ContainsAny(resource, "*?") {
		return false
	}
	return strings.HasPrefix(resource, "role/")
}

// isCloudFormationStackARN reports whether value is a CloudFormation stack ARN:
// the service segment is exactly cloudformation and the resource segment names
// a stack (stack/...). The CloudFormation scanner publishes a stack node's
// resource_id as the stack ARN, so the provisioned-product->stack edge keys on
// this exact shape to resolve instead of dangling.
func isCloudFormationStackARN(value string) bool {
	if arnService(value) != "cloudformation" {
		return false
	}
	return strings.HasPrefix(arnResource(value), "stack/")
}

func timeOrNil(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}
