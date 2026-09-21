// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package datazone

import (
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// domainResourceID returns the resource_id the domain node publishes. It prefers
// the DataZone domain id (always present, and the value child resources
// reference) and falls back to the domain ARN, so child-in-domain edges key the
// domain by the same value the node publishes.
func domainResourceID(domain Domain) string {
	return firstNonEmpty(domain.ID, domain.ARN)
}

// redshiftClusterARN synthesizes the partition-aware provisioned Redshift
// cluster ARN for clusterName so a data-source-backs-cluster edge matches the
// Redshift scanner's published cluster resource_id, which is the cluster ARN it
// synthesizes as
// arn:<partition>:redshift:<region>:<account>:cluster:<identifier>. The DataZone
// data source reports a bare cluster name plus an optional backing account and
// region; account and region default to the scan boundary. It returns "" when
// the cluster name is empty.
func redshiftClusterARN(boundary awscloud.Boundary, clusterName, account, region string) string {
	clusterName = strings.TrimSpace(clusterName)
	if clusterName == "" {
		return ""
	}
	account = strings.TrimSpace(account)
	if account == "" {
		account = strings.TrimSpace(boundary.AccountID)
	}
	region = strings.TrimSpace(region)
	if region == "" {
		region = strings.TrimSpace(boundary.Region)
	}
	partition := awscloud.PartitionForRegion(region)
	if partition == "" {
		partition = awscloud.PartitionForBoundary(boundary)
	}
	return "arn:" + partition + ":redshift:" + region + ":" + account + ":cluster:" + clusterName
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

// cloneStrings returns a trimmed copy of input with empty entries dropped and
// duplicates collapsed, or nil when nothing survives.
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
		if _, exists := seen[trimmed]; exists {
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
