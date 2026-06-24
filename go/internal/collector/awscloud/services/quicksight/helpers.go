// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package quicksight

import (
	"strings"
	"time"
)

// dataSourceResourceID returns the resource_id the data source node publishes.
// It prefers the data source ARN (always present from the QuickSight API) and
// falls back to the data source id, so dataset->data-source edges can key the
// data source by the same value the node publishes.
func dataSourceResourceID(dataSource DataSource) string {
	return firstNonEmpty(dataSource.ARN, dataSource.ID)
}

// dataSetResourceID returns the resource_id the dataset node publishes. It
// prefers the dataset ARN and falls back to the dataset id, so dashboard and
// analysis edges can key the dataset by the same value the node publishes.
func dataSetResourceID(dataSet DataSet) string {
	return firstNonEmpty(dataSet.ARN, dataSet.ID)
}

// dashboardResourceID returns the resource_id the dashboard node publishes,
// preferring the dashboard ARN and falling back to the dashboard id.
func dashboardResourceID(dashboard Dashboard) string {
	return firstNonEmpty(dashboard.ARN, dashboard.ID)
}

// analysisResourceID returns the resource_id the analysis node publishes,
// preferring the analysis ARN and falling back to the analysis id.
func analysisResourceID(analysis Analysis) string {
	return firstNonEmpty(analysis.ARN, analysis.ID)
}

// arnForBucket synthesizes the partition-aware S3 bucket ARN for name, or
// returns an already-formed ARN unchanged. S3 buckets have no API ARN, so the
// scanner synthesizes one carrying the boundary partition (aws / aws-cn /
// aws-us-gov) so the data-source->bucket target matches the S3 scanner's
// published bucket resource_id in every partition instead of dangling the edge.
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

// vpcConnectionIDFromARN extracts the bare VPC connection id from a QuickSight
// VPC connection ARN. QuickSight VPC connection ARNs end with
// ".../vpcConnection/<id>"; the resolved VPC connection summary is keyed by that
// bare id. It returns "" when value is empty or not a recognizable ARN.
func vpcConnectionIDFromARN(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || !strings.HasPrefix(value, "arn:") {
		return ""
	}
	if idx := strings.LastIndex(value, "/"); idx >= 0 && idx+1 < len(value) {
		return strings.TrimSpace(value[idx+1:])
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

// dedupeStrings returns a trimmed, order-preserving, de-duplicated copy of
// input with empty entries dropped, or nil when nothing survives. Physical
// tables and dashboard versions can reference the same data source or dataset
// more than once, so edges must not duplicate.
func dedupeStrings(input []string) []string {
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
