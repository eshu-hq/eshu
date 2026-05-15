package mcp

import "strconv"

func packageRegistryDependenciesRoute(args map[string]any) *route {
	return &route{method: "GET", path: "/api/v0/package-registry/dependencies", query: map[string]string{
		"after_dependency_id": str(args, "after_dependency_id"),
		"after_version_id":    str(args, "after_version_id"),
		"limit":               strconv.Itoa(intOr(args, "limit", 50)),
		"package_id":          str(args, "package_id"),
		"version_id":          str(args, "version_id"),
	}}
}

func packageRegistryCorrelationsRoute(args map[string]any) *route {
	return &route{method: "GET", path: "/api/v0/package-registry/correlations", query: map[string]string{
		"after_correlation_id": str(args, "after_correlation_id"),
		"limit":                strconv.Itoa(intOr(args, "limit", 50)),
		"package_id":           str(args, "package_id"),
		"relationship_kind":    str(args, "relationship_kind"),
		"repository_id":        str(args, "repository_id"),
	}}
}
