// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicecontexttools

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/mcp/contract/route"
	"github.com/eshu-hq/eshu/go/internal/workloadid"
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
		request, err := investigateServiceRoute(args)
		return request, true, err
	default:
		return routecontract.Request{}, false, nil
	}
}

// serviceIntelligenceReportRoute resolves the get_service_intelligence_report
// tool to GET /api/v0/services/{service_name}/intelligence-report, mirroring
// the service-story selector (service_id, workload_id, or service_name, plus
// optional repo and environment) so the report and the story address the same
// service.
func serviceIntelligenceReportRoute(args routecontract.Arguments) (routecontract.Request, error) {
	selector := firstSelector(args, "service_id", "workload_id", "service_name")
	if selector == "" {
		return routecontract.Request{}, fmt.Errorf("get_service_intelligence_report requires service_id, workload_id, or service_name")
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
// get_service_context accepts only workload_id. get_service_story accepts
// service_id, workload_id, or service_name, checked in that order: the exact
// id is the most specific selector, so adding it to an ambiguous name call
// resolves the call. A workload:* selector is also forwarded as the service_id
// query, and a repository selector (repo, repository_id, or repo_id) as repo,
// so a repository-scoped caller gets the same disambiguation path as API
// callers.
func serviceSelectorRoute(args routecontract.Arguments, toolName string, suffix string) (routecontract.Request, error) {
	keys := []string{"workload_id"}
	if suffix == "story" {
		keys = []string{"service_id", "workload_id", "service_name"}
	}
	selector := firstSelector(args, keys...)
	if selector == "" {
		if suffix == "story" {
			return routecontract.Request{}, fmt.Errorf("%s requires service_id, workload_id, or service_name", toolName)
		}
		return routecontract.Request{}, fmt.Errorf("%s requires workload_id", toolName)
	}
	q := map[string]string{}
	if env := args.String("environment"); env != "" {
		q["environment"] = env
	}
	if suffix == "context" {
		// #7129: the MCP default is handles so a populated service fits the
		// response budget; the HTTP default stays full. An explicit value wins,
		// and the handler rejects an unknown one.
		q["evidence_detail"] = "handles"
		if detail := args.String("evidence_detail"); detail != "" {
			q["evidence_detail"] = detail
		}
	}
	if suffix == "story" {
		// #8798: the story defaults to handles over MCP for the same reason
		// the context route does (#7129): a large service's full dossier
		// exceeds the response budget. An explicit value wins.
		q["evidence_detail"] = "handles"
		if detail := args.String("evidence_detail"); detail != "" {
			q["evidence_detail"] = detail
		}
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
// /api/v0/investigations/services/{service_name}. It accepts service_id or
// service_name, checked in that order. workload_id is deliberately not an
// investigate_service argument (its schema does not advertise it). The schema
// marks neither required, since service_id may stand alone, so a missing
// selector is refused here by name instead of reaching the handler as an empty
// path segment.
func investigateServiceRoute(args routecontract.Arguments) (routecontract.Request, error) {
	serviceName := firstSelector(args, "service_id", "service_name")
	if serviceName == "" {
		return routecontract.Request{}, fmt.Errorf("investigate_service requires service_id or service_name")
	}
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
	}, nil
}

// firstSelector returns the first non-blank argument among keys, trimmed. It is
// the one place the service tools' selector precedence lives: service_id, then
// workload_id, then service_name, for the tools that accept each.
func firstSelector(args routecontract.Arguments, keys ...string) string {
	for _, key := range keys {
		if selector := strings.TrimSpace(args.String(key)); selector != "" {
			return selector
		}
	}
	return ""
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
// service identifier (for example "service:payments-api" -> "payments-api")
// before it is used as an HTTP path segment. A workload: selector — the
// public handle or the full scoped id per workloadid.ClassifyWorkloadSelector
// — is forwarded whole so the API handle ladder resolves it server-side
// (#5385). A value with no colon, or an empty head or tail around the colon,
// passes through unchanged.
func normalizeQualifiedIdentifier(value string) string {
	switch workloadid.ClassifyWorkloadSelector(value) {
	case workloadid.SelectorKindHandle, workloadid.SelectorKindFullID:
		return value
	default:
		if head, tail, ok := strings.Cut(value, ":"); ok && head != "" && tail != "" {
			return tail
		}
		return value
	}
}

// canonicalWorkloadIdentifier returns value unchanged when it is a workload:
// handle or full scoped id per workloadid.ClassifyWorkloadSelector, or an
// empty string otherwise, so a route builder forwards it as a service_id
// query parameter only when it is genuinely a workload-qualified identifier.
func canonicalWorkloadIdentifier(value string) string {
	switch workloadid.ClassifyWorkloadSelector(value) {
	case workloadid.SelectorKindHandle, workloadid.SelectorKindFullID:
		return value
	default:
		return ""
	}
}
