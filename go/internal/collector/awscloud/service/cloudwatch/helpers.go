// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloudwatch

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// dimensionSummary returns a stable []any of {name, value} pairs for one
// metric alarm's dimensions. Dimension names that look like AWS-managed
// system dimensions (e.g. InstanceId, ClusterName, FunctionName) pass through
// as plain strings. Dimension names that look like customer tags route their
// value through the shared redact library before persistence.
//
// The classification is intentionally narrow: only a small allow-list of AWS
// system dimensions is treated as non-tag; everything else is redacted. This
// fails closed for unknown shapes, matching the redact ruleset policy for
// unknown provider schemas.
func dimensionSummary(dimensions []MetricDimension, key redact.Key) []any {
	if len(dimensions) == 0 {
		return nil
	}
	out := make([]any, 0, len(dimensions))
	for _, dim := range dimensions {
		name := strings.TrimSpace(dim.Name)
		value := strings.TrimSpace(dim.Value)
		if name == "" {
			continue
		}
		entry := map[string]any{
			"name": name,
		}
		if dimensionIsCustomerTagShaped(name) && !key.IsZero() {
			entry["value"] = awscloud.RedactString(value, "cloudwatch.alarm.dimension."+name, key)
		} else {
			entry["value"] = value
		}
		out = append(out, entry)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// dimensionIsCustomerTagShaped reports whether a dimension key name looks
// like a customer tag rather than an AWS-managed system dimension. Known AWS
// system dimensions stay in the clear; everything else is treated as a
// customer-controlled label that may carry sensitive payload.
func dimensionIsCustomerTagShaped(name string) bool {
	switch strings.TrimSpace(name) {
	case "":
		return false
	}
	// Anchored AWS system dimensions. The list is conservative: when AWS adds
	// new system dimensions and we have not updated this allow-list, the
	// scanner redacts rather than leaks.
	systemDimensions := map[string]struct{}{
		"AutoScalingGroupName": {},
		"BucketName":           {},
		"CacheClusterId":       {},
		"ClusterName":          {},
		"DBClusterIdentifier":  {},
		"DBInstanceIdentifier": {},
		"DistributionId":       {},
		"FilterName":           {},
		"FunctionName":         {},
		"InstanceId":           {},
		"LambdaName":           {},
		"LoadBalancer":         {},
		"LogGroupName":         {},
		"NamespaceName":        {},
		"QueueName":            {},
		"Resource":             {},
		"ServiceName":          {},
		"StateMachineArn":      {},
		"TableName":            {},
		"TargetGroup":          {},
		"TopicName":            {},
		"TransitGatewayId":     {},
		"VolumeId":             {},
		"VpcId":                {},
		"WebACL":               {},
	}
	if _, ok := systemDimensions[name]; ok {
		return false
	}
	return true
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func joinNonEmpty(separator string, values ...string) string {
	var parts []string
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return strings.Join(parts, separator)
}

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
