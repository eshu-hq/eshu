// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package route53recoverycontrolconfig

import "strings"

// clusterResourceID returns the resource_id the cluster node publishes. The
// cluster ARN is the stable join key, so control-panel-in-cluster edges key the
// cluster by the same value the node publishes.
func clusterResourceID(cluster Cluster) string {
	return firstNonEmpty(cluster.ARN, cluster.Name)
}

// controlPanelResourceID returns the resource_id the control panel node
// publishes. The control panel ARN is the stable join key, so routing-control
// and safety-rule edges key the panel by the same value the node publishes.
func controlPanelResourceID(panel ControlPanel) string {
	return firstNonEmpty(panel.ARN, panel.Name)
}

// routingControlResourceID returns the resource_id the routing control node
// publishes, preferring its ARN.
func routingControlResourceID(control RoutingControl) string {
	return firstNonEmpty(control.ARN, control.Name)
}

// safetyRuleResourceID returns the resource_id the safety rule node publishes,
// preferring its ARN.
func safetyRuleResourceID(rule SafetyRule) string {
	return firstNonEmpty(rule.ARN, rule.Name)
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
