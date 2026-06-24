// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fis

import (
	"strings"
	"time"
)

// templateResourceID returns the resource_id the experiment-template node
// publishes. It prefers the template ARN (always present from the FIS API) and
// falls back to the template id, so a template's own edges are sourced on the
// same id the template node publishes.
func templateResourceID(template ExperimentTemplate) string {
	return firstNonEmpty(template.ARN, template.ID)
}

// arnForBucket synthesizes the partition-aware S3 bucket ARN for name, or
// returns an already-formed ARN unchanged. S3 buckets have no API ARN, so the
// scanner synthesizes one carrying the boundary partition (aws / aws-cn /
// aws-us-gov) so the template->bucket target matches the S3 scanner's published
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

// instanceIDFromARN extracts the bare EC2 instance id (i-...) from an EC2
// instance ARN's instance/<id> resource segment. EC2 instance graph nodes are
// keyed by the bare instance id, not the ARN, so the FIS EC2 target edge must
// publish the bare id to join. It returns "" when arn is not an EC2 instance
// ARN.
func instanceIDFromARN(arn string) string {
	arn = strings.TrimSpace(arn)
	if !strings.HasPrefix(arn, "arn:") {
		return ""
	}
	marker := ":instance/"
	idx := strings.Index(arn, marker)
	if idx < 0 {
		return ""
	}
	id := strings.TrimSpace(arn[idx+len(marker):])
	if !strings.HasPrefix(id, "i-") {
		return ""
	}
	return id
}

// arnService returns the service segment of an AWS ARN (the third colon-
// delimited field), lowercased, or "" when value is not an ARN with a service
// segment. The scanner uses it to type an FIS target resource ARN to the right
// resource family (ecs, rds) before keying the edge.
func arnService(value string) string {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "arn:") {
		return ""
	}
	parts := strings.SplitN(value, ":", 4)
	if len(parts) < 3 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(parts[2]))
}

// arnResourceSegment returns the resource segment of an AWS ARN (everything
// after the fifth colon), or "" when value is not an ARN with a resource
// segment. The scanner uses it to distinguish RDS DB instance ARNs (db:...)
// from DB cluster ARNs (cluster:...).
func arnResourceSegment(value string) string {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "arn:") {
		return ""
	}
	parts := strings.SplitN(value, ":", 6)
	if len(parts) < 6 {
		return ""
	}
	return strings.TrimSpace(parts[5])
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
