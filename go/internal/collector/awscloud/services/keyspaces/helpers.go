// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package keyspaces

import (
	"strings"
	"time"
)

// columnMaps renders structural column definitions (name + CQL type) into safe
// attribute maps. It carries no table row or cell data.
func columnMaps(columns []Column) []map[string]string {
	if len(columns) == 0 {
		return nil
	}
	output := make([]map[string]string, 0, len(columns))
	for _, column := range columns {
		name := strings.TrimSpace(column.Name)
		if name == "" {
			continue
		}
		output = append(output, map[string]string{
			"name": name,
			"type": strings.TrimSpace(column.Type),
		})
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

// clusteringKeyMaps renders clustering-key definitions (column name + sort
// order) into safe attribute maps. It carries no table row or cell data.
func clusteringKeyMaps(keys []ClusteringKey) []map[string]string {
	if len(keys) == 0 {
		return nil
	}
	output := make([]map[string]string, 0, len(keys))
	for _, key := range keys {
		name := strings.TrimSpace(key.Name)
		if name == "" {
			continue
		}
		output = append(output, map[string]string{
			"name":     name,
			"order_by": strings.TrimSpace(key.OrderBy),
		})
	}
	if len(output) == 0 {
		return nil
	}
	return output
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func timeOrNil(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}
