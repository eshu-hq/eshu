// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dms

import (
	"strings"
	"time"
)

// instanceResourceID returns the resource_id a replication instance node
// publishes. It prefers the instance ARN (always present from
// DescribeReplicationInstances) and falls back to the customer identifier so a
// task's runs-on-instance edge can key the instance by the same value.
func instanceResourceID(instance ReplicationInstance) string {
	return firstNonEmpty(instance.ARN, instance.Identifier)
}

// subnetGroupResourceID returns the resource_id a replication subnet group node
// publishes. DMS reports no subnet-group ARN, so the node is keyed by its
// identifier and the instance-in-subnet-group edge keys it the same way.
func subnetGroupResourceID(group ReplicationSubnetGroup) string {
	return strings.TrimSpace(group.Identifier)
}

// endpointResourceID returns the resource_id an endpoint node publishes. It
// prefers the endpoint ARN (always present from DescribeEndpoints) and falls
// back to the customer identifier so a task's source/target endpoint edges can
// key the endpoint by the same value.
func endpointResourceID(endpoint Endpoint) string {
	return firstNonEmpty(endpoint.ARN, endpoint.Identifier)
}

// taskResourceID returns the resource_id a replication task node publishes. It
// prefers the task ARN and falls back to the customer identifier.
func taskResourceID(task ReplicationTask) string {
	return firstNonEmpty(task.ARN, task.Identifier)
}

// arnForBucket synthesizes the partition-aware S3 bucket ARN for name, or
// returns an already-formed ARN unchanged. S3 buckets have no API ARN, so the
// scanner synthesizes one carrying the boundary partition (aws / aws-cn /
// aws-us-gov) so the endpoint->bucket target matches the S3 scanner's published
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

// arnIfARN returns value when it is ARN-shaped, otherwise the empty string, so
// a relationship sets target_arn only for ARN-shaped target identifiers.
func arnIfARN(value string) string {
	value = strings.TrimSpace(value)
	if isARN(value) {
		return value
	}
	return ""
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

// cloneStrings returns a trimmed copy of input with empty entries dropped, or
// nil when nothing survives.
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
