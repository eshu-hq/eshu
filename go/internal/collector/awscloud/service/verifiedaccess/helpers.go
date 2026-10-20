// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package verifiedaccess

import (
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// resourceARN synthesizes the partition-aware EC2 Verified Access ARN for a
// resource of the given kind and id, or returns "" when the id is empty.
// Verified Access instances, endpoints, and trust providers carry no ARN in the
// EC2 describe responses, so the scanner derives the ARN the resource node
// publishes from the boundary partition, region, account, and id rather than
// hardcoding arn:aws:. The ARN shape is
// arn:<partition>:ec2:<region>:<account>:<kind>/<id>.
func resourceARN(boundary awscloud.Boundary, kind, id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	partition := awscloud.PartitionForBoundary(boundary)
	region := strings.TrimSpace(boundary.Region)
	account := strings.TrimSpace(boundary.AccountID)
	return "arn:" + partition + ":ec2:" + region + ":" + account + ":" + kind + "/" + id
}

// instanceResourceID returns the resource_id the instance node publishes. It is
// the synthesized partition-aware instance ARN when account/region are known,
// falling back to the bare instance id, so group-in-instance edges can key the
// instance by the same value the node publishes.
func instanceResourceID(boundary awscloud.Boundary, instanceID string) string {
	if arn := resourceARN(boundary, "verified-access-instance", instanceID); arn != "" && hasIdentity(boundary) {
		return arn
	}
	return strings.TrimSpace(instanceID)
}

// groupResourceID returns the resource_id the group node publishes. It prefers
// the API-reported group ARN and falls back to the synthesized partition-aware
// ARN, then the bare group id, so endpoint-in-group edges key the group by the
// same value the node publishes.
func groupResourceID(boundary awscloud.Boundary, group Group) string {
	if arn := strings.TrimSpace(group.ARN); arn != "" {
		return arn
	}
	if arn := resourceARN(boundary, "verified-access-group", group.ID); arn != "" && hasIdentity(boundary) {
		return arn
	}
	return strings.TrimSpace(group.ID)
}

// trustProviderResourceID returns the resource_id the trust-provider node
// publishes: the synthesized partition-aware trust-provider ARN, falling back to
// the bare trust-provider id, so instance-uses-trust-provider edges key the
// node by the same value it publishes.
func trustProviderResourceID(boundary awscloud.Boundary, trustProviderID string) string {
	if arn := resourceARN(boundary, "verified-access-trust-provider", trustProviderID); arn != "" && hasIdentity(boundary) {
		return arn
	}
	return strings.TrimSpace(trustProviderID)
}

// hasIdentity reports whether the boundary carries the account and region the
// scanner needs to synthesize a fully-qualified ARN. Without both, the scanner
// keys nodes and edges by the bare id instead of an under-specified ARN.
func hasIdentity(boundary awscloud.Boundary) bool {
	return strings.TrimSpace(boundary.AccountID) != "" && strings.TrimSpace(boundary.Region) != ""
}

// isARN reports whether value carries the canonical AWS ARN prefix.
func isARN(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), "arn:")
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
