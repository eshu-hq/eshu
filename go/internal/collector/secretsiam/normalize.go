// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package secretsiam

import (
	"sort"
	"strings"
)

const wildcardAction = "*"

func normalizeEffect(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "allow":
		return "Allow"
	case "deny":
		return "Deny"
	default:
		return ""
	}
}

func normalizeActionList(values []string) []string {
	return normalizeStrings(values, strings.ToLower)
}

func normalizePatternList(values []string) []string {
	return normalizeStrings(values, nil)
}

func normalizeKeyList(values []string) []string {
	return normalizeStrings(values, nil)
}

func normalizeStrings(values []string, mapFn func(string) string) []string {
	seen := make(map[string]struct{}, len(values))
	output := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if mapFn != nil {
			trimmed = mapFn(trimmed)
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		output = append(output, trimmed)
	}
	sort.Strings(output)
	return output
}

func containsValue(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func sourceRecordID(candidate, fallback string) string {
	candidate = strings.TrimSpace(candidate)
	if candidate != "" {
		return candidate
	}
	return strings.TrimSpace(fallback)
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func trustPolicySourceID(roleARN, statementSID, effect string, actions []string) string {
	return strings.Join([]string{roleARN, PolicySourceTrust, statementSID, effect, strings.Join(actions, ",")}, "#")
}

func permissionPolicySourceID(
	principalARN string,
	policySource string,
	policyARN string,
	policyName string,
	statementSID string,
	effect string,
	actions []string,
) string {
	policyRef := policyARN
	if strings.TrimSpace(policyRef) == "" {
		policyRef = policyName
	}
	return strings.Join([]string{principalARN, policySource, policyRef, statementSID, effect, strings.Join(actions, ",")}, "#")
}

func cloneAnyMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}
