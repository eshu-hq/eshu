// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// ServiceQueryEvidence groups content-derived service evidence before it is
// shaped into service context, service story, or deployment trace responses.
type ServiceQueryEvidence struct {
	Hostnames            []ServiceHostnameEvidence            `json:"hostnames,omitempty"`
	Environments         []ServiceEnvironmentEvidence         `json:"environments,omitempty"`
	DocsRoutes           []ServiceDocsRouteEvidence           `json:"docs_routes,omitempty"`
	APISpecs             []ServiceAPISpecEvidence             `json:"api_specs,omitempty"`
	FrameworkRoutes      []FrameworkRouteEvidence             `json:"framework_routes,omitempty"`
	EntrypointCandidates []ServiceEntrypointCandidateEvidence `json:"entrypoint_candidates,omitempty"`
}

// ServiceHostnameEvidence is exact hostname evidence that may become a public
// service entrypoint.
type ServiceHostnameEvidence struct {
	Hostname     string `json:"hostname"`
	Environment  string `json:"environment,omitempty"`
	RelativePath string `json:"relative_path"`
	Reason       string `json:"reason"`
}

// ServiceEnvironmentEvidence captures an environment signal from service
// content paths, file bodies, or exact hostname evidence.
type ServiceEnvironmentEvidence struct {
	Environment  string `json:"environment"`
	RelativePath string `json:"relative_path"`
	Reason       string `json:"reason"`
}

// ServiceDocsRouteEvidence captures documented internal docs/spec routes.
type ServiceDocsRouteEvidence struct {
	Route        string `json:"route"`
	RelativePath string `json:"relative_path"`
	Reason       string `json:"reason"`
}

// ServiceEntrypointCandidateEvidence preserves hostname-shaped candidates that
// are rejected or ambiguous and therefore must not become public entrypoints.
type ServiceEntrypointCandidateEvidence struct {
	Candidate      string `json:"candidate"`
	Classification string `json:"classification"`
	RelativePath   string `json:"relative_path"`
	Reason         string `json:"reason"`
}

// ServiceAPISpecEvidence summarizes one API spec file and its parsed routes,
// server hostnames, and operation IDs when available.
type ServiceAPISpecEvidence struct {
	RelativePath     string                       `json:"relative_path"`
	Format           string                       `json:"format"`
	Parsed           bool                         `json:"parsed"`
	SpecVersion      string                       `json:"spec_version,omitempty"`
	APIVersion       string                       `json:"api_version,omitempty"`
	EndpointCount    int                          `json:"endpoint_count,omitempty"`
	MethodCount      int                          `json:"method_count,omitempty"`
	OperationIDCount int                          `json:"operation_id_count,omitempty"`
	DocsRoutes       []string                     `json:"docs_routes,omitempty"`
	Hostnames        []string                     `json:"hostnames,omitempty"`
	Endpoints        []ServiceAPIEndpointEvidence `json:"endpoints,omitempty"`
}

// ServiceAPIEndpointEvidence captures one API endpoint path from an API spec.
type ServiceAPIEndpointEvidence struct {
	Path         string   `json:"path"`
	Methods      []string `json:"methods,omitempty"`
	OperationIDs []string `json:"operation_ids,omitempty"`
}

// FrameworkRouteEvidence captures routes detected by parser framework_semantics
// from fact_records.
type FrameworkRouteEvidence struct {
	Framework    string                        `json:"framework"`
	RelativePath string                        `json:"relative_path"`
	RoutePaths   []string                      `json:"route_paths"`
	RouteMethods []string                      `json:"route_methods"`
	RouteEntries []FrameworkRouteEntryEvidence `json:"route_entries,omitempty"`
}

// FrameworkRouteEntryEvidence preserves one parser-observed route declaration.
// Handler is the route's handler function symbol when the parser observed an
// exact binding; it is omitted for inline or middleware-wrapped routes whose
// handler is ambiguous (#2721).
type FrameworkRouteEntryEvidence struct {
	Method  string `json:"method"`
	Path    string `json:"path"`
	Handler string `json:"handler,omitempty"`
}
