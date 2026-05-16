package mcp

import "strconv"

func packageRegistryPackagesRoute(args map[string]any) *route {
	return &route{method: "GET", path: "/api/v0/package-registry/packages", query: map[string]string{
		"ecosystem":  str(args, "ecosystem"),
		"limit":      strconv.Itoa(intOr(args, "limit", 50)),
		"name":       str(args, "name"),
		"package_id": str(args, "package_id"),
	}}
}

func packageRegistryVersionsRoute(args map[string]any) *route {
	return &route{method: "GET", path: "/api/v0/package-registry/versions", query: map[string]string{
		"limit":      strconv.Itoa(intOr(args, "limit", 50)),
		"package_id": str(args, "package_id"),
	}}
}

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
