// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package elb

import "strings"

const (
	// ec2InstanceTargetType is the target_type for the load-balancer-registers-
	// instance edge. Eshu does not emit an aws_ec2_instance resource yet, so this
	// is a documented forward reference in relguard.KnownTargetTypeAllowlist; the
	// id keyed is the bare instance id (i-...).
	ec2InstanceTargetType = "aws_ec2_instance"
	// iamServerCertificateTargetType is the target_type for an HTTPS/SSL listener
	// whose certificate is an IAM server certificate. Eshu does not scan an IAM
	// server-certificate resource yet, so this is a documented forward reference
	// in relguard.KnownTargetTypeAllowlist.
	iamServerCertificateTargetType = "aws_iam_server_certificate"
)

// isARN reports whether value has the canonical AWS ARN prefix.
func isARN(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), "arn:")
}

// arnService returns the service segment of an ARN
// (arn:partition:service:region:account:resource), or "" when value is not an
// ARN with at least the service field. It lower-cases the segment so the caller
// can compare against canonical service names.
func arnService(value string) string {
	parts := strings.SplitN(strings.TrimSpace(value), ":", 4)
	if len(parts) < 3 || parts[0] != "arn" {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(parts[2]))
}

// dedupe trims, drops empties, and removes duplicate values while preserving
// first-seen order. It keeps relationship emission idempotent when AWS reports
// the same id twice.
func dedupe(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	output := make([]string, 0, len(values))
	for _, value := range values {
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

// nonEmptyStrings returns the trimmed, non-empty values from input, preserving
// order and duplicates.
func nonEmptyStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	output := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			output = append(output, trimmed)
		}
	}
	return output
}

// cloneStrings returns a defensive copy of input, or nil when input is empty, so
// attribute maps do not alias scanner-owned slices.
func cloneStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, len(input))
	copy(output, input)
	return output
}
