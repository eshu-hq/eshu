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

func serviceSelectorRoute(args map[string]any, toolName string, suffix string) (*route, error) {
	selector := strings.TrimSpace(str(args, "workload_id"))
	if selector == "" {
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
	}
	return &route{
		method: "GET",
		path:   "/api/v0/services/" + url.PathEscape(normalizeQualifiedIdentifier(selector)) + "/" + suffix,
		query:  q,
	}, nil
}
