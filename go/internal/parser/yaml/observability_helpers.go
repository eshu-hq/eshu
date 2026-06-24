// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package yaml

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

func decodeJSONObject(value string) (map[string]any, error) {
	result := map[string]any{}
	if err := json.Unmarshal([]byte(value), &result); err != nil {
		return nil, err
	}
	return result, nil
}

func looksLikeGrafanaDashboard(value map[string]any) bool {
	if cleanString(value["uid"]) != "" || cleanString(value["title"]) != "" {
		return true
	}
	if _, ok := value["panels"].([]any); ok {
		return true
	}
	return false
}

func collectDashboardDatasourceRefs(dashboard map[string]any) []string {
	var refs []string
	var walk func(any)
	walk = func(value any) {
		switch typed := value.(type) {
		case map[string]any:
			if datasource, ok := typed["datasource"]; ok {
				refs = append(refs, datasourceRefs(datasource)...)
			}
			for _, item := range typed {
				walk(item)
			}
		case []any:
			for _, item := range typed {
				walk(item)
			}
		}
	}
	walk(dashboard)
	return sortedUniqueStrings(refs)
}

func collectAlertDatasourceRefs(rule map[string]any) []string {
	var refs []string
	if items, ok := rule["data"].([]any); ok {
		for _, item := range items {
			data, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if uid := cleanString(data["datasourceUid"]); uid != "" {
				refs = append(refs, "uid:"+uid)
			}
		}
	}
	return sortedUniqueStrings(refs)
}

func datasourceRefs(value any) []string {
	switch typed := value.(type) {
	case map[string]any:
		var refs []string
		if uid := cleanString(typed["uid"]); uid != "" {
			refs = append(refs, "uid:"+uid)
		}
		if kind := strings.ToLower(cleanString(typed["type"])); kind != "" {
			refs = append(refs, "type:"+kind)
		}
		return refs
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}
		return []string{"name_fingerprint:" + fingerprintValue(typed)}
	default:
		return nil
	}
}

func collectDashboardServiceHints(dashboard map[string]any, labels map[string]any) []string {
	var hints []string
	for _, key := range []string{"app.kubernetes.io/name", "app", "service", "service.name"} {
		if value := cleanString(labels[key]); value != "" {
			hints = append(hints, value)
		}
	}
	if tags, ok := dashboard["tags"].([]any); ok {
		for _, item := range tags {
			tag := cleanString(item)
			lower := strings.ToLower(tag)
			for _, prefix := range []string{"service:", "app:"} {
				if strings.HasPrefix(lower, prefix) {
					hints = append(hints, strings.TrimSpace(tag[len(prefix):]))
				}
			}
		}
	}
	return sortedUniqueStrings(hints)
}

func datasourceRedactedFields(row map[string]any) []string {
	var redacted []string
	for _, key := range sortedMapKeysAny(row) {
		lower := strings.ToLower(key)
		if lower == "url" || strings.Contains(lower, "secret") ||
			strings.Contains(lower, "password") || strings.Contains(lower, "token") ||
			strings.Contains(lower, "securejsondata") ||
			strings.Contains(lower, "secure_json_data") {
			redacted = append(redacted, key)
		}
	}
	return sortedUniqueStrings(redacted)
}

func alertRedactedFields(rule map[string]any) []string {
	var redacted []string
	if _, ok := rule["data"]; ok {
		redacted = append(redacted, "data.model")
	}
	return redacted
}

func cleanString(value any) string {
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" || text == "<nil>" {
		return ""
	}
	return text
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func fingerprintValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.ToLower(value)))
	return hex.EncodeToString(sum[:])[:16]
}

func fingerprintObject(value any) string {
	data, err := json.Marshal(value)
	if err != nil || len(data) == 0 || string(data) == "null" {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])[:16]
}

func safeNamePart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", " ", "_", ".", "_")
	return replacer.Replace(value)
}

func cleanStringList(values []any) []string {
	if len(values) == 0 {
		return nil
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if cleaned := cleanString(value); cleaned != "" {
			result = append(result, cleaned)
		}
	}
	return sortedUniqueStrings(result)
}

func sortedUniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	for _, value := range values {
		if cleaned := strings.TrimSpace(value); cleaned != "" {
			seen[cleaned] = struct{}{}
		}
	}
	result := make([]string, 0, len(seen))
	for value := range seen {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func environmentFromPath(path string) string {
	parts := strings.Split(filepath.ToSlash(path), "/")
	for index, part := range parts {
		if part == "environments" && index+1 < len(parts) {
			return strings.TrimSpace(parts[index+1])
		}
	}
	return ""
}
