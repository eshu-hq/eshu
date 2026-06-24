// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package imagebuilder

import (
	"strings"
	"time"
)

// trimSpace is a local alias for strings.TrimSpace so the package's identity and
// timestamp helpers read consistently without importing strings everywhere.
func trimSpace(value string) string {
	return strings.TrimSpace(value)
}

// isARN reports whether value carries the canonical AWS ARN prefix.
func isARN(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), "arn:")
}

// arnForInstanceProfile synthesizes the partition-aware IAM instance-profile ARN
// the IAM scanner publishes as an instance-profile node's resource_id, or
// returns an already-formed ARN unchanged. Image Builder reports the instance
// profile NAME, so the scanner derives the ARN carrying the boundary partition
// (aws / aws-cn / aws-us-gov) and account so the edge joins the real
// instance-profile node in every partition instead of dangling. It returns ""
// when the name or account is missing.
func arnForInstanceProfile(partition, accountID, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if strings.HasPrefix(name, "arn:") {
		return name
	}
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return ""
	}
	return "arn:" + partition + ":iam::" + accountID + ":instance-profile/" + name
}

// arnForBucket synthesizes the partition-aware S3 bucket ARN the S3 scanner
// publishes as a bucket node's resource_id, or returns an already-formed ARN
// unchanged. S3 buckets have no API ARN, so the scanner synthesizes one carrying
// the boundary partition so the infra-config->bucket target joins the real
// bucket node in every partition. It returns "" when name is blank.
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

// arnForECRRepository synthesizes the partition-aware ECR repository ARN the ECR
// scanner publishes as a repository node's resource_id, or returns an
// already-formed ARN unchanged. Image Builder reports the ECR repository NAME,
// so the scanner derives the ARN carrying the boundary partition, region, and
// account so the container-recipe->repository edge joins the real repository
// node. It returns "" when the name, region, or account is missing.
func arnForECRRepository(partition, region, accountID, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if strings.HasPrefix(name, "arn:") {
		return name
	}
	region = strings.TrimSpace(region)
	accountID = strings.TrimSpace(accountID)
	if region == "" || accountID == "" {
		return ""
	}
	return "arn:" + partition + ":ecr:" + region + ":" + accountID + ":repository/" + name
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
