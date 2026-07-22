// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func discoverProbeSelectors(client *http.Client, apiBaseURL, userToken string) (map[string]string, error) {
	selectors := map[string]string{
		"relative_path": "README.md",
		"search_term":   "main",
		"language":      "go",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		strings.TrimRight(apiBaseURL, "/")+"/api/v0/repositories?limit=1",
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("build bounded selector discovery request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+userToken)
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("bounded selector discovery: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("bounded selector discovery HTTP status %d", response.StatusCode)
	}
	var payload any
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode bounded selector discovery: %w", err)
	}
	collectSelectorValues(payload, selectors)
	collectRepositorySelector(payload, selectors)
	discoverAdditionalSelectors(client, apiBaseURL, userToken, selectors)
	return selectors, nil
}

type selectorDiscoverySource struct {
	path    string
	classes map[string][]string
}

func discoverAdditionalSelectors(client *http.Client, apiBaseURL, userToken string, selectors map[string]string) {
	sources := []selectorDiscoverySource{
		{path: "/api/v0/graph/entities?kind=services&limit=1", classes: map[string][]string{
			"service_id": {"service_id", "id"}, "service_name": {"service_name", "name"},
		}},
		{path: "/api/v0/cloud/resources?limit=1", classes: map[string][]string{
			"cloud_resource_id": {"resource_id", "id"}, "scope_id": {"scope_id"},
		}},
		{path: "/api/v0/package-registry/packages?limit=1", classes: map[string][]string{
			"package_id": {"package_id", "uid", "id"},
		}},
		{path: "/api/v0/collectors?limit=1", classes: map[string][]string{
			"collector_family": {"family", "collector_kind", "kind"},
		}},
		{path: "/api/v0/documentation/findings?limit=1", classes: map[string][]string{
			"finding_id": {"finding_id", "id"}, "packet_id": {"packet_id"},
		}},
		{path: "/api/v0/component-extensions?limit=1", classes: map[string][]string{
			"component_id": {"component_id", "id"},
		}},
		{path: "/api/v0/investigation-workflows?limit=1", classes: map[string][]string{
			"workflow_id": {"workflow_id", "id"},
		}},
		{path: "/api/v0/query-playbooks?limit=1", classes: map[string][]string{
			"playbook_id": {"playbook_id", "id"},
		}},
	}
	for _, source := range sources {
		payload, err := fetchOptionalSelectorSource(client, apiBaseURL, userToken, source.path)
		if err != nil {
			continue
		}
		collectSelectorValues(payload, selectors)
		for class, keys := range source.classes {
			if _, exists := selectors[class]; exists {
				continue
			}
			if value := findFirstStringField(payload, keys); value != "" {
				selectors[class] = value
			}
		}
	}
}

func fetchOptionalSelectorSource(client *http.Client, apiBaseURL, userToken, path string) (any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(apiBaseURL, "/")+path, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+userToken)
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("HTTP status %d", response.StatusCode)
	}
	var payload any
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func findFirstStringField(value any, keys []string) string {
	switch typed := value.(type) {
	case map[string]any:
		for _, key := range keys {
			if text, ok := typed[key].(string); ok && strings.TrimSpace(text) != "" {
				return text
			}
		}
		for _, child := range typed {
			if text := findFirstStringField(child, keys); text != "" {
				return text
			}
		}
	case []any:
		for _, child := range typed {
			if text := findFirstStringField(child, keys); text != "" {
				return text
			}
		}
	}
	return ""
}

func collectSelectorValues(value any, selectors map[string]string) {
	switch typed := value.(type) {
	case map[string]any:
		for name, child := range typed {
			if text, ok := child.(string); ok && strings.TrimSpace(text) != "" {
				class := selectorClass(name)
				if class != name || isDeclaredSelectorClass(name) {
					if _, exists := selectors[class]; !exists {
						selectors[class] = text
					}
				}
			}
			collectSelectorValues(child, selectors)
		}
	case []any:
		for _, child := range typed {
			collectSelectorValues(child, selectors)
		}
	}
}

func collectRepositorySelector(value any, selectors map[string]string) {
	root, ok := value.(map[string]any)
	if !ok {
		return
	}
	for _, key := range []string{"repositories", "repos", "items"} {
		rows, ok := root[key].([]any)
		if !ok || len(rows) == 0 {
			continue
		}
		row, ok := rows[0].(map[string]any)
		if !ok {
			continue
		}
		for _, idKey := range []string{"repository_id", "repo_id", "id"} {
			if id, ok := row[idKey].(string); ok && id != "" {
				selectors["repository_id"] = id
				break
			}
		}
		for _, nameKey := range []string{"repo_name", "name", "slug"} {
			if name, ok := row[nameKey].(string); ok && name != "" {
				selectors["repository_name"] = name
				break
			}
		}
		return
	}
}

func isDeclaredSelectorClass(name string) bool {
	for _, class := range []string{
		"repository_id", "repository_name", "service_id", "service_name", "workload_id", "entity_id",
		"cloud_resource_id", "scope_id", "generation_id", "terraform_state_scope", "package_id", "fact_kind",
		"collector_family", "component_id", "finding_id", "packet_id", "incident_id", "relationship_id",
		"workflow_id", "playbook_id",
	} {
		if name == class {
			return true
		}
	}
	return false
}

func resolveProbeSelectors(probe graphReadProbe, selectors map[string]string) (graphReadProbe, error) {
	resolved := probe
	path, err := resolveSelectorString(probe.path, selectors, true)
	if err != nil {
		return graphReadProbe{}, err
	}
	resolved.path = path
	resolved.query, err = resolveSelectorMap(probe.query, selectors)
	if err != nil {
		return graphReadProbe{}, err
	}
	resolved.arguments, err = resolveSelectorMap(probe.arguments, selectors)
	if err != nil {
		return graphReadProbe{}, err
	}
	return resolved, nil
}

func resolveSelectorMap(values map[string]any, selectors map[string]string) (map[string]any, error) {
	if values == nil {
		return nil, nil
	}
	resolved := make(map[string]any, len(values))
	for name, value := range values {
		item, err := resolveSelectorValue(value, selectors)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		resolved[name] = item
	}
	return resolved, nil
}

func resolveSelectorValue(value any, selectors map[string]string) (any, error) {
	switch typed := value.(type) {
	case string:
		return resolveSelectorString(typed, selectors, false)
	case map[string]any:
		return resolveSelectorMap(typed, selectors)
	case []any:
		resolved := make([]any, 0, len(typed))
		for _, item := range typed {
			value, err := resolveSelectorValue(item, selectors)
			if err != nil {
				return nil, err
			}
			resolved = append(resolved, value)
		}
		return resolved, nil
	default:
		return value, nil
	}
}

func resolveSelectorString(value string, selectors map[string]string, pathEscape bool) (string, error) {
	for {
		start := strings.Index(value, selectorPrefix)
		if start < 0 {
			return value, nil
		}
		endOffset := strings.Index(value[start:], "}}")
		if endOffset < 0 {
			return "", fmt.Errorf("malformed selector placeholder")
		}
		end := start + endOffset + 2
		class := value[start+len(selectorPrefix) : start+endOffset]
		replacement, ok := selectors[class]
		if !ok || replacement == "" {
			return "", fmt.Errorf("missing discovered selector class %q", class)
		}
		if pathEscape {
			replacement = url.PathEscape(replacement)
		}
		value = value[:start] + replacement + value[end:]
	}
}
