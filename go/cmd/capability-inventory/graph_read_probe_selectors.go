// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func discoverProbeSelectors(client *http.Client, apiBaseURL, userToken string) (map[string]string, error) {
	selectors := map[string]string{
		"search_term": "main",
		"language":    "go",
		// tag has no bounded live-discovery source: ContainerImageTagObservation
		// rows come only from the opt-in oci_registry collector (off in a
		// default deploy), and the identity read model
		// (ContainerImageIdentityRow) does not expose a tag field to discover
		// one from. "latest" mirrors the generic, non-private defaults above
		// (a common branch name, a common language) rather than inventing a
		// discovery call for data that a default deployment does not have.
		"tag": "latest",
		// oci_repository_id has no bounded live-discovery source either, and it
		// is NOT interchangeable with the discovered "repository_id" class: the
		// repository-list endpoint discovers a git-shaped identity
		// (repository:r_<hash>), but TagHistoryHandler.listTagHistory
		// (go/internal/query/tag_history.go) requires an oci-registry://-shaped
		// id via composeOCIImageRef and returns HTTP 400 otherwise. The static
		// default below mirrors the same fixture value already used by
		// go/internal/query/tag_history_test.go for a valid oci-registry:// id.
		"oci_repository_id": "oci-registry://ghcr.io/eshu-hq/demo",
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
		return nil, errors.New("build bounded selector discovery request failed")
	}
	request.Header.Set("Authorization", "Bearer "+userToken)
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return nil, errors.New("bounded selector discovery request failed")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("bounded selector discovery HTTP status %d", response.StatusCode)
	}
	var payload any
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode bounded selector discovery: %w", err)
	}
	collectRepositorySelector(payload, selectors)
	discoverRepositoryScopedSelectors(client, apiBaseURL, userToken, selectors)
	discoverAdditionalSelectors(client, apiBaseURL, userToken, selectors)
	return selectors, nil
}

type selectorDiscoverySource struct {
	path    string
	classes map[string][]string
}

func discoverRepositoryScopedSelectors(client *http.Client, apiBaseURL, userToken string, selectors map[string]string) {
	repositoryID := selectors["repository_id"]
	if repositoryID == "" {
		return
	}
	for _, source := range []selectorDiscoverySource{
		{
			path: "/api/v0/freshness/generations?repository=" + url.QueryEscape(repositoryID) + "&limit=1",
			classes: map[string][]string{
				"scope_id": {"scope_id"}, "generation_id": {"generation_id"},
			},
		},
		{
			path: "/api/v0/repositories/" + url.PathEscape(repositoryID) + "/context",
			classes: map[string][]string{
				"relationship_id": {"resolved_id"},
			},
		},
		{
			path: "/api/v0/service-catalog/correlations?repository_id=" + url.QueryEscape(repositoryID) + "&limit=1",
			classes: map[string][]string{
				"workload_id": {"workload_id"}, "service_id": {"service_id"}, "service_name": {"display_name"},
			},
		},
	} {
		collectSelectorsFromSource(client, apiBaseURL, userToken, selectors, source)
	}
	for _, source := range []struct {
		path    string
		body    map[string]any
		classes map[string][]string
	}{
		{
			path: "/api/v0/content/entities/search",
			body: map[string]any{"repo_id": repositoryID, "query": "main", "limit": 1},
			classes: map[string][]string{
				"entity_id": {"entity_id", "id"},
			},
		},
		{
			path: "/api/v0/content/files/search",
			body: map[string]any{"repo_id": repositoryID, "query": "main", "limit": 1},
			classes: map[string][]string{
				"relative_path": {"relative_path", "path"},
			},
		},
	} {
		payload, err := fetchOptionalSelectorJSONSource(client, apiBaseURL, userToken, source.path, source.body)
		if err != nil {
			continue
		}
		collectSelectorClasses(payload, selectors, source.classes)
	}
	payload, err := fetchOptionalSelectorSource(client, apiBaseURL, userToken, "/api/v0/freshness/generations?limit=500")
	if err == nil {
		if scope := findFirstStringWithPrefix(payload, []string{"scope_id"}, "state_snapshot:"); scope != "" {
			selectors["terraform_state_scope"] = scope
		}
	}
}

func discoverAdditionalSelectors(client *http.Client, apiBaseURL, userToken string, selectors map[string]string) {
	sources := []selectorDiscoverySource{
		{path: "/api/v0/cloud/inventory?limit=1", classes: map[string][]string{
			"cloud_resource_id": {"cloud_resource_uid", "resource_id", "id"},
		}},
		{path: "/api/v0/cloud/resources?limit=1", classes: map[string][]string{
			"arn": {"arn"},
		}},
		{path: "/api/v0/fact-schema-versions?limit=1", classes: map[string][]string{
			"fact_kind": {"fact_kind"},
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
		collectSelectorsFromSource(client, apiBaseURL, userToken, selectors, source)
	}
}

func collectSelectorsFromSource(
	client *http.Client,
	apiBaseURL string,
	userToken string,
	selectors map[string]string,
	source selectorDiscoverySource,
) {
	payload, err := fetchOptionalSelectorSource(client, apiBaseURL, userToken, source.path)
	if err != nil {
		return
	}
	collectSelectorClasses(payload, selectors, source.classes)
}

func collectSelectorClasses(payload any, selectors map[string]string, classes map[string][]string) {
	for class, keys := range classes {
		if _, exists := selectors[class]; exists {
			continue
		}
		if value := findFirstStringField(payload, keys); value != "" {
			selectors[class] = value
		}
	}
}

func fetchOptionalSelectorSource(client *http.Client, apiBaseURL, userToken, path string) (any, error) {
	return fetchOptionalSelectorRequest(client, apiBaseURL, userToken, http.MethodGet, path, nil)
}

func fetchOptionalSelectorJSONSource(
	client *http.Client,
	apiBaseURL string,
	userToken string,
	path string,
	payload map[string]any,
) (any, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return fetchOptionalSelectorRequest(client, apiBaseURL, userToken, http.MethodPost, path, bytes.NewReader(body))
}

func fetchOptionalSelectorRequest(
	client *http.Client,
	apiBaseURL string,
	userToken string,
	method string,
	path string,
	body io.Reader,
) (any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(apiBaseURL, "/")+path, body)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+userToken)
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
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

func findFirstStringWithPrefix(value any, keys []string, prefix string) string {
	switch typed := value.(type) {
	case map[string]any:
		for _, key := range keys {
			if text, ok := typed[key].(string); ok && strings.HasPrefix(text, prefix) {
				return text
			}
		}
		for _, child := range typed {
			if text := findFirstStringWithPrefix(child, keys, prefix); text != "" {
				return text
			}
		}
	case []any:
		for _, child := range typed {
			if text := findFirstStringWithPrefix(child, keys, prefix); text != "" {
				return text
			}
		}
	}
	return ""
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
