package mcp

import (
	"net/url"
	"strconv"
)

func documentationRoute(toolName string, args map[string]any) (*route, bool) {
	switch toolName {
	case "list_documentation_findings":
		return &route{method: "GET", path: "/api/v0/documentation/findings", query: documentationFindingsQuery(args)}, true
	case "get_documentation_evidence_packet":
		return &route{
			method: "GET",
			path:   "/api/v0/documentation/findings/" + url.PathEscape(str(args, "finding_id")) + "/evidence-packet",
		}, true
	case "check_documentation_evidence_packet_freshness":
		query := map[string]string{}
		if version := str(args, "packet_version"); version != "" {
			query["packet_version"] = version
		}
		return &route{
			method: "GET",
			path:   "/api/v0/documentation/evidence-packets/" + url.PathEscape(str(args, "packet_id")) + "/freshness",
			query:  query,
		}, true
	default:
		return nil, false
	}
}

func documentationFindingsQuery(args map[string]any) map[string]string {
	query := map[string]string{}
	for _, key := range []string{
		"finding_type",
		"source_id",
		"document_id",
		"status",
		"truth_level",
		"freshness_state",
		"updated_since",
		"cursor",
	} {
		if value := str(args, key); value != "" {
			query[key] = value
		}
	}
	if limit := intOr(args, "limit", 50); limit > 0 {
		query["limit"] = strconv.Itoa(limit)
	}
	return query
}
