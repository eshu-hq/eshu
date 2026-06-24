// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"fmt"
	"net/url"
	"strings"
)

func serviceContextRoute(args map[string]any) (*route, error) {
	return serviceSelectorRoute(args, "get_service_context", "context")
}

func serviceStoryRoute(args map[string]any) (*route, error) {
	return serviceSelectorRoute(args, "get_service_story", "story")
}

// serviceIntelligenceReportRoute resolves the get_service_intelligence_report
// tool to GET /api/v0/services/{service_name}/intelligence-report, mirroring the
// service-story selector (workload_id or service_name, plus optional service_id,
// repo, and environment) so the report and the story address the same service.
func serviceIntelligenceReportRoute(args map[string]any) (*route, error) {
	selector := strings.TrimSpace(str(args, "workload_id"))
	if selector == "" {
		selector = strings.TrimSpace(str(args, "service_name"))
	}
	if selector == "" {
		return nil, fmt.Errorf("get_service_intelligence_report requires workload_id or service_name")
	}
	q := map[string]string{}
	if env := str(args, "environment"); env != "" {
		q["environment"] = env
	}
	if serviceID := canonicalWorkloadIdentifier(selector); serviceID != "" {
		q["service_id"] = serviceID
	}
	if repo := serviceStoryRepositorySelector(args); repo != "" {
		q["repo"] = repo
	}
	return &route{
		method: "GET",
		path:   "/api/v0/services/" + url.PathEscape(normalizeQualifiedIdentifier(selector)) + "/intelligence-report",
		query:  q,
	}, nil
}

func serviceSelectorRoute(args map[string]any, toolName string, suffix string) (*route, error) {
	selector := strings.TrimSpace(str(args, "workload_id"))
	if selector == "" && suffix == "story" {
		selector = strings.TrimSpace(str(args, "service_name"))
	}
	if selector == "" {
		if suffix == "story" {
			return nil, fmt.Errorf("%s requires workload_id or service_name", toolName)
		}
		return nil, fmt.Errorf("%s requires workload_id", toolName)
	}
	q := map[string]string{}
	if env := str(args, "environment"); env != "" {
		q["environment"] = env
	}
	if suffix == "story" {
		if serviceID := canonicalWorkloadIdentifier(selector); serviceID != "" {
			q["service_id"] = serviceID
		}
		if repo := serviceStoryRepositorySelector(args); repo != "" {
			q["repo"] = repo
		}
	}
	return &route{
		method: "GET",
		path:   "/api/v0/services/" + url.PathEscape(normalizeQualifiedIdentifier(selector)) + "/" + suffix,
		query:  q,
	}, nil
}

func serviceStoryRepositorySelector(args map[string]any) string {
	for _, key := range []string{"repo", "repository_id", "repo_id"} {
		if selector := strings.TrimSpace(str(args, key)); selector != "" {
			return selector
		}
	}
	return ""
}
