// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicecontexttools

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// Route selects the internal HTTP request for a service-context tool
// without executing it. It reports handled only for the four tools this
// package owns: get_service_context, get_service_story,
// get_service_intelligence_report, and investigate_service. Family
// membership is an explicit name switch, never a prefix match. Unlike the
// sibling route-selector packages (deadcode, codequality, entityresolution,
// codeintel, iacmanagement), get_service_context and get_service_story
// validate their selector before returning, so Route reports handled=true
// with a non-nil error rather than silently building a request with an
// empty path segment -- callers must check the error even when handled is
// true, exactly as relationships.EdgeRoute does for list_relationship_edges.
func Route(toolName string, args routecontract.Arguments) (routecontract.Request, bool, error) {
	switch toolName {
	case "get_service_context":
		request, err := serviceSelectorRoute(args, "get_service_context", "context")
		return request, true, err
	case "get_service_story":
		request, err := serviceSelectorRoute(args, "get_service_story", "story")
		return request, true, err
	case "get_service_intelligence_report":
		request, err := serviceIntelligenceReportRoute(args)
		return request, true, err
	case "investigate_service":
		return investigateServiceRoute(args), true, nil
	default:
		return routecontract.Request{}, false, nil
	}
}

// serviceIntelligenceReportRoute resolves the get_service_intelligence_report
// tool to GET /api/v0/services/{service_name}/intelligence-report, mirroring
// the service-story selector (workload_id or service_name, plus optional
// service_id, repo, and environment) so the report and the story address the
// same service.
func serviceIntelligenceReportRoute(args routecontract.Arguments) (routecontract.Request, error) {
	selector := strings.TrimSpace(args.String("workload_id"))
	if selector == "" {
		selector = strings.TrimSpace(args.String("service_name"))
	}
	if selector == "" {
		return routecontract.Request{}, fmt.Errorf("get_service_intelligence_report requires workload_id or service_name")
	}
	q := map[string]string{}
	if env := args.String("environment"); env != "" {
		q["environment"] = env
	}
	if serviceID := canonicalWorkloadIdentifier(selector); serviceID != "" {
		q["service_id"] = serviceID
	}
	if repo := serviceStoryRepositorySelector(args); repo != "" {
		q["repo"] = repo
	}
	return routecontract.Request{
		Method: "GET",
		Path:   "/api/v0/services/" + url.PathEscape(normalizeQualifiedIdentifier(selector)) + "/intelligence-report",
		Query:  q,
	}, nil
}

// serviceSelectorRoute resolves get_service_context (suffix "context") and
// get_service_story (suffix "story") to GET /api/v0/services/{selector}/...
// get_service_context accepts only workload_id; get_service_story also
// falls back to service_name, and additionally forwards a canonical
// workload:* selector as service_id and a repository selector (repo,
// repository_id, or repo_id) as repo, so a repository-scoped caller gets the
// same disambiguation path as API callers.
func serviceSelectorRoute(args routecontract.Arguments, toolName string, suffix string) (routecontract.Request, error) {
	selector := strings.TrimSpace(args.String("workload_id"))
	if selector == "" && suffix == "story" {
		selector = strings.TrimSpace(args.String("service_name"))
	}
	if selector == "" {
		if suffix == "story" {
			return routecontract.Request{}, fmt.Errorf("%s requires workload_id or service_name", toolName)
		}
		return routecontract.Request{}, fmt.Errorf("%s requires workload_id", toolName)
	}
	q := map[string]string{}
	if env := args.String("environment"); env != "" {
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
	return routecontract.Request{
		Method: "GET",
		Path:   "/api/v0/services/" + url.PathEscape(normalizeQualifiedIdentifier(selector)) + "/" + suffix,
		Query:  q,
	}, nil
}

// investigateServiceRoute resolves investigate_service to GET
// /api/v0/investigations/services/{service_name}. Unlike
// serviceSelectorRoute and serviceIntelligenceReportRoute, it does not
// require a non-empty selector -- a blank service_name reaches the query
// handler as an empty path segment, matching the pre-extraction root switch
// arm this replaces.
func investigateServiceRoute(args routecontract.Arguments) routecontract.Request {
	serviceName := args.String("service_name")
	q := map[string]string{
		"environment": args.String("environment"),
		"intent":      args.String("intent"),
		"question":    args.String("question"),
	}
	if serviceID := canonicalWorkloadIdentifier(serviceName); serviceID != "" {
		q["service_id"] = serviceID
	}
	if repo := serviceStoryRepositorySelector(args); repo != "" {
		q["repo"] = repo
	}
	return routecontract.Request{
		Method: "GET",
		Path:   "/api/v0/investigations/services/" + url.PathEscape(normalizeQualifiedIdentifier(serviceName)),
		Query:  q,
	}
}

// serviceStoryRepositorySelector returns the first non-blank repository
// selector among repo, repository_id, and repo_id, matching the alias order
// the pre-extraction root switch checked.
func serviceStoryRepositorySelector(args routecontract.Arguments) string {
	for _, key := range []string{"repo", "repository_id", "repo_id"} {
		if selector := strings.TrimSpace(args.String(key)); selector != "" {
			return selector
		}
	}
	return ""
}

// normalizeQualifiedIdentifier strips a "<type>:" prefix from a qualified
// service identifier (for example "workload:payments-api" ->
// "payments-api") before it is used as an HTTP path segment. A value with no
// colon, or an empty head or tail around the colon, passes through
// unchanged.
func normalizeQualifiedIdentifier(value string) string {
	if head, tail, ok := strings.Cut(value, ":"); ok && head != "" && tail != "" {
		return tail
	}
	return value
}

// canonicalWorkloadIdentifier returns value unchanged when it carries the
// canonical "workload:" prefix, or an empty string otherwise, so a
// route builder can forward it as a service_id query parameter only when it
// is genuinely a workload-qualified identifier.
func canonicalWorkloadIdentifier(value string) string {
	if head, tail, ok := strings.Cut(value, ":"); ok && head == "workload" && tail != "" {
		return value
	}
	return ""
}
