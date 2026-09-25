// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package appstream

import (
	"strings"
	"time"
)

// fleetResourceID returns the resource_id the fleet node publishes. It prefers
// the fleet ARN (always present from DescribeFleets) and falls back to the fleet
// name, so a fleet's own edges key on the same value the fleet node publishes.
func fleetResourceID(fleet Fleet) string {
	return firstNonEmpty(fleet.ARN, fleet.Name)
}

// stackResourceID returns the resource_id the stack node publishes. It prefers
// the stack ARN and falls back to the stack name.
func stackResourceID(stack Stack) string {
	return firstNonEmpty(stack.ARN, stack.Name)
}

// imageBuilderResourceID returns the resource_id the image builder node
// publishes. It prefers the image builder ARN and falls back to the name.
func imageBuilderResourceID(builder ImageBuilder) string {
	return firstNonEmpty(builder.ARN, builder.Name)
}

// imageResourceID returns the resource_id the image node publishes. It prefers
// the image ARN and falls back to the image name, so a fleet-to-image or
// builder-to-image edge keyed on the reported image ARN joins the image node.
func imageResourceID(image Image) string {
	return firstNonEmpty(image.ARN, image.Name)
}

// arnForBucket synthesizes the partition-aware S3 bucket ARN for name, or
// returns an already-formed ARN unchanged. S3 buckets have no API ARN, so the
// scanner synthesizes one carrying the boundary partition (aws / aws-cn /
// aws-us-gov) so the stack->bucket target matches the S3 scanner's published
// bucket resource_id in every partition instead of dangling the graph edge.
func arnForBucket(partition, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if strings.HasPrefix(name, "arn:") {
		return name
	}
	return "arn:" + partition + ":s3:::" + name
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

// cloneStrings returns a trimmed copy of input with empty and duplicate entries
// dropped, or nil when nothing survives. De-duplication keeps repeated subnet,
// security group, or bucket identifiers from emitting duplicate edges.
func cloneStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(input))
	output := make([]string, 0, len(input))
	for _, value := range input {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		output = append(output, trimmed)
	}
	if len(output) == 0 {
		return nil
	}
	return output
}
