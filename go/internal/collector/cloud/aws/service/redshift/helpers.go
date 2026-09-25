// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package redshift

import "strings"

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
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

func parameterGroupIdentityMap(groups []ClusterParameterGroup) map[string]string {
	identities := make(map[string]string, len(groups))
	for _, group := range groups {
		name := strings.TrimSpace(group.Name)
		id := firstNonEmpty(group.ARN, name)
		if name != "" && id != "" {
			identities[name] = id
		}
	}
	return identities
}

func subnetGroupIdentityMap(groups []ClusterSubnetGroup) map[string]string {
	identities := make(map[string]string, len(groups))
	for _, group := range groups {
		name := strings.TrimSpace(group.Name)
		id := firstNonEmpty(group.ARN, name)
		if name != "" && id != "" {
			identities[name] = id
		}
	}
	return identities
}

func clusterIdentityMap(clusters []Cluster) map[string]string {
	identities := make(map[string]string, len(clusters))
	for _, cluster := range clusters {
		identifier := strings.TrimSpace(cluster.Identifier)
		id := firstNonEmpty(cluster.ARN, identifier)
		if identifier != "" && id != "" {
			identities[identifier] = id
		}
	}
	return identities
}

func namespaceIdentityMap(namespaces []ServerlessNamespace) map[string]string {
	identities := make(map[string]string, len(namespaces))
	for _, namespace := range namespaces {
		name := strings.TrimSpace(namespace.Name)
		id := firstNonEmpty(namespace.ARN, name)
		if name != "" && id != "" {
			identities[name] = id
		}
	}
	return identities
}

func configParameterMaps(parameters []ServerlessConfigParameter) []map[string]any {
	if len(parameters) == 0 {
		return nil
	}
	output := make([]map[string]any, 0, len(parameters))
	for _, parameter := range parameters {
		key := strings.TrimSpace(parameter.Key)
		if key == "" {
			continue
		}
		output = append(output, map[string]any{
			"parameter_key":   key,
			"parameter_value": strings.TrimSpace(parameter.Value),
		})
	}
	if len(output) == 0 {
		return nil
	}
	return output
}
