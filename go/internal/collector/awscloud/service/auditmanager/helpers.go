// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package auditmanager

import (
	"strings"
	"time"
)

// assessmentResourceID returns the resource_id the assessment node publishes. It
// prefers the assessment ARN (always present from GetAssessment) and falls back
// to the assessment id, so an assessment's own edges are sourced on the same id
// the assessment node publishes.
func assessmentResourceID(assessment Assessment) string {
	return firstNonEmpty(assessment.ARN, assessment.ID)
}

// frameworkResourceID returns the resource_id the framework node publishes. It
// prefers the framework ARN and falls back to the framework id.
func frameworkResourceID(framework Framework) string {
	return firstNonEmpty(framework.ARN, framework.ID)
}

// controlResourceID returns the resource_id the control node publishes. It
// prefers the control ARN and falls back to the control id.
func controlResourceID(control Control) string {
	return firstNonEmpty(control.ARN, control.ID)
}

// arnForBucket synthesizes the partition-aware S3 bucket ARN for name, or
// returns an already-formed ARN unchanged. S3 buckets have no API ARN, so the
// scanner synthesizes one carrying the boundary partition (aws / aws-cn /
// aws-us-gov) so the assessment->bucket target matches the S3 scanner's
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

// accountRootARN synthesizes the partition-aware account root ARN
// (arn:<partition>:iam::<account-id>:root) for an in-scope account id. This is
// the identity the config, access-analyzer, and ds scanners use to target the
// aws_account resource family, so the assessment->account edge joins the same
// node. It returns "" for an empty account id.
func accountRootARN(partition, accountID string) string {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return ""
	}
	return "arn:" + partition + ":iam::" + accountID + ":root"
}

// bucketNameFromS3URI extracts the bucket name from an s3:// destination URI, or
// returns "" when value is not an s3:// URI. Audit Manager reports the
// assessment-reports destination as an s3://bucket/prefix URI, and only the
// bucket name is needed to key the S3 bucket node.
func bucketNameFromS3URI(value string) string {
	trimmed := strings.TrimSpace(value)
	const scheme = "s3://"
	if !strings.HasPrefix(strings.ToLower(trimmed), scheme) {
		return ""
	}
	rest := trimmed[len(scheme):]
	if slash := strings.IndexByte(rest, '/'); slash >= 0 {
		rest = rest[:slash]
	}
	return strings.TrimSpace(rest)
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
// every key trims to empty.
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
