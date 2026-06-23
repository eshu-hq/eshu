package query

import (
	"sort"
	"strings"
)

func buildServiceHostnameRows(rows []ServiceHostnameEvidence) []map[string]any {
	if len(rows) == 0 {
		return nil
	}
	result := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		result = append(result, map[string]any{
			"hostname":      row.Hostname,
			"environment":   row.Environment,
			"relative_path": row.RelativePath,
			"reason":        row.Reason,
		})
	}
	return result
}

func buildServiceEntrypointCandidateRows(rows []ServiceEntrypointCandidateEvidence) []map[string]any {
	if len(rows) == 0 {
		return nil
	}
	result := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		result = append(result, map[string]any{
			"candidate":      row.Candidate,
			"classification": row.Classification,
			"relative_path":  row.RelativePath,
			"reason":         row.Reason,
		})
	}
	return result
}

func hostnameLabels(rows []map[string]any) []string {
	if len(rows) == 0 {
		return nil
	}
	values := make([]string, 0, len(rows))
	for _, row := range rows {
		hostname := StringVal(row, "hostname")
		if hostname == "" {
			continue
		}
		values = append(values, hostname)
	}
	return uniqueSortedStrings(values)
}

func buildServiceAPISurface(evidence ServiceQueryEvidence) map[string]any {
	if len(evidence.APISpecs) == 0 && len(evidence.DocsRoutes) == 0 && len(evidence.FrameworkRoutes) == 0 {
		return nil
	}

	docsRoutes := serviceEvidenceDocsRoutes(evidence)
	hostnames := serviceEvidenceHostnames(evidence)
	specPaths := make([]string, 0, len(evidence.APISpecs))
	specVersions := make([]string, 0, len(evidence.APISpecs))
	apiVersions := make([]string, 0, len(evidence.APISpecs))
	endpoints := make([]map[string]any, 0)
	endpointCount := 0
	methodCount := 0
	operationIDCount := 0
	parsedSpecCount := 0
	for _, spec := range evidence.APISpecs {
		specPaths = append(specPaths, spec.RelativePath)
		endpointCount += spec.EndpointCount
		methodCount += spec.MethodCount
		operationIDCount += spec.OperationIDCount
		if spec.Parsed {
			parsedSpecCount++
		}
		if spec.SpecVersion != "" {
			specVersions = append(specVersions, spec.SpecVersion)
		}
		if spec.APIVersion != "" {
			apiVersions = append(apiVersions, spec.APIVersion)
		}
		for _, endpoint := range spec.Endpoints {
			endpoints = append(endpoints, map[string]any{
				"path":          endpoint.Path,
				"methods":       append([]string(nil), endpoint.Methods...),
				"operation_ids": append([]string(nil), endpoint.OperationIDs...),
				"spec_path":     spec.RelativePath,
			})
		}
	}
	sort.Strings(specPaths)
	sort.Slice(endpoints, func(i, j int) bool {
		if StringVal(endpoints[i], "path") != StringVal(endpoints[j], "path") {
			return StringVal(endpoints[i], "path") < StringVal(endpoints[j], "path")
		}
		return StringVal(endpoints[i], "spec_path") < StringVal(endpoints[j], "spec_path")
	})

	// Merge framework-detected routes into the endpoint list.
	frameworkRouteCount := 0
	frameworkSet := map[string]struct{}{}
	for _, fr := range evidence.FrameworkRoutes {
		frameworkSet[fr.Framework] = struct{}{}
		frameworkEndpoints := frameworkRouteEndpoints(fr)
		for _, endpoint := range frameworkEndpoints {
			endpoints = append(endpoints, map[string]any{
				"path":      endpoint.Path,
				"methods":   lowerStrings(endpoint.Methods),
				"source":    "framework",
				"framework": fr.Framework,
				"spec_path": fr.RelativePath,
			})
			frameworkRouteCount++
		}
	}
	frameworks := make([]string, 0, len(frameworkSet))
	for fw := range frameworkSet {
		frameworks = append(frameworks, fw)
	}
	sort.Strings(frameworks)

	// Re-sort endpoints after framework routes added.
	sort.Slice(endpoints, func(i, j int) bool {
		if StringVal(endpoints[i], "path") != StringVal(endpoints[j], "path") {
			return StringVal(endpoints[i], "path") < StringVal(endpoints[j], "path")
		}
		return StringVal(endpoints[i], "spec_path") < StringVal(endpoints[j], "spec_path")
	})

	result := map[string]any{
		"spec_count":         len(evidence.APISpecs),
		"parsed_spec_count":  parsedSpecCount,
		"spec_paths":         uniqueSortedStrings(specPaths),
		"spec_versions":      uniqueSortedStrings(specVersions),
		"api_versions":       uniqueSortedStrings(apiVersions),
		"endpoint_count":     endpointCount,
		"method_count":       methodCount,
		"operation_id_count": operationIDCount,
		"docs_routes":        docsRoutes,
		"hostnames":          hostnames,
		"endpoints":          endpoints,
	}
	if frameworkRouteCount > 0 {
		result["framework_route_count"] = frameworkRouteCount
		result["frameworks"] = frameworks
	}
	return result
}

type frameworkRouteEndpoint struct {
	Path    string
	Methods []string
}

// frameworkRouteEndpoints uses paired parser evidence when available so method
// lists stay attached to the route path where they were declared.
func frameworkRouteEndpoints(fr FrameworkRouteEvidence) []frameworkRouteEndpoint {
	if len(fr.RouteEntries) == 0 {
		endpoints := make([]frameworkRouteEndpoint, 0, len(fr.RoutePaths))
		for _, routePath := range fr.RoutePaths {
			endpoints = append(endpoints, frameworkRouteEndpoint{
				Path:    routePath,
				Methods: fr.RouteMethods,
			})
		}
		return endpoints
	}

	methodsByPath := make(map[string][]string, len(fr.RouteEntries))
	for _, entry := range fr.RouteEntries {
		path := strings.TrimSpace(entry.Path)
		method := strings.TrimSpace(entry.Method)
		if path == "" || method == "" {
			continue
		}
		methodsByPath[path] = append(methodsByPath[path], method)
	}
	paths := make([]string, 0, len(methodsByPath))
	for path := range methodsByPath {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	endpoints := make([]frameworkRouteEndpoint, 0, len(paths))
	for _, path := range paths {
		endpoints = append(endpoints, frameworkRouteEndpoint{
			Path:    path,
			Methods: uniqueSortedStrings(methodsByPath[path]),
		})
	}
	return endpoints
}

func serviceEvidenceHostnames(evidence ServiceQueryEvidence) []string {
	values := make([]string, 0, len(evidence.Hostnames)+len(evidence.APISpecs))
	for _, row := range evidence.Hostnames {
		values = append(values, row.Hostname)
	}
	for _, spec := range evidence.APISpecs {
		values = append(values, spec.Hostnames...)
	}
	return uniqueSortedStrings(values)
}

func serviceEvidenceDocsRoutes(evidence ServiceQueryEvidence) []string {
	values := make([]string, 0, len(evidence.DocsRoutes)+len(evidence.APISpecs))
	for _, row := range evidence.DocsRoutes {
		values = append(values, row.Route)
	}
	for _, spec := range evidence.APISpecs {
		values = append(values, spec.DocsRoutes...)
	}
	return uniqueSortedStrings(values)
}

func serviceEvidenceEnvironmentNames(rows []ServiceEnvironmentEvidence) []string {
	values := make([]string, 0, len(rows))
	for _, row := range rows {
		values = append(values, row.Environment)
	}
	return uniqueSortedStrings(values)
}

func lowerStrings(values []string) []string {
	result := make([]string, len(values))
	for i, v := range values {
		result[i] = strings.ToLower(v)
	}
	sort.Strings(result)
	return result
}

func uniqueSortedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
