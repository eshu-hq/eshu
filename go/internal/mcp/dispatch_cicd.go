package mcp

import "strconv"

func cicdRunCorrelationsRoute(args map[string]any) *route {
	return &route{method: "GET", path: "/api/v0/ci-cd/run-correlations", query: map[string]string{
		"after_correlation_id": str(args, "after_correlation_id"),
		"artifact_digest":      str(args, "artifact_digest"),
		"commit_sha":           str(args, "commit_sha"),
		"environment":          str(args, "environment"),
		"limit":                strconv.Itoa(intOr(args, "limit", 50)),
		"outcome":              str(args, "outcome"),
		"provider_run_id":      cicdProviderRunID(args),
		"repository_id":        str(args, "repository_id"),
		"run_id":               str(args, "run_id"),
		"scope_id":             str(args, "scope_id"),
	}}
}

func cicdProviderRunID(args map[string]any) string {
	if value := str(args, "provider_run_id"); value != "" {
		return value
	}
	return str(args, "run_id")
}
