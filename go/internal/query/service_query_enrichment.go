package query

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
)

type serviceQueryEnrichmentOptions struct {
	DirectOnly                bool
	IncludeRelatedModuleUsage bool
	MaxDepth                  int
	Logger                    *slog.Logger
	Operation                 string
}

func enrichServiceQueryContext(
	ctx context.Context,
	graph GraphQuery,
	content ContentStore,
	workloadContext map[string]any,
) error {
	return enrichServiceQueryContextWithOptions(ctx, graph, content, workloadContext, serviceQueryEnrichmentOptions{
		IncludeRelatedModuleUsage: true,
		Operation:                 "service_context",
	})
}

func enrichServiceQueryContextWithOptions(
	ctx context.Context,
	graph GraphQuery,
	content ContentStore,
	workloadContext map[string]any,
	opts serviceQueryEnrichmentOptions,
) error {
	delete(workloadContext, "entry_points")
	if len(workloadContext) == 0 {
		return nil
	}

	repoID := safeStr(workloadContext, "repo_id")
	serviceName := safeStr(workloadContext, "name")
	operation := opts.Operation
	if operation == "" {
		operation = "service_context"
	}
	timer := startServiceQueryStage(ctx, opts.Logger, operation, serviceName, repoID, "graph_api_surface")
	if graphAPISurface := queryServiceGraphAPISurface(ctx, graph, repoID); len(graphAPISurface) > 0 {
		workloadContext["api_surface"] = graphAPISurface
	}
	timer.Done(ctx, slog.Bool("has_result", len(mapValue(workloadContext, "api_surface")) > 0))
	timer = startServiceQueryStage(ctx, opts.Logger, operation, serviceName, repoID, "graph_deployment_evidence")
	graphEvidence, err := queryServiceGraphDeploymentEvidence(ctx, graph, content, repoID)
	if err != nil {
		timer.Done(ctx, slog.Bool("error", true))
		return fmt.Errorf("load graph deployment evidence: %w", err)
	}
	if len(graphEvidence) > 0 {
		workloadContext["deployment_evidence"] = graphEvidence
	}
	timer.Done(ctx, slog.Bool("has_result", len(mapValue(workloadContext, "deployment_evidence")) > 0))
	if repoID == "" || serviceName == "" || content == nil {
		return nil
	}

	timer = startServiceQueryStage(ctx, opts.Logger, operation, serviceName, repoID, "service_evidence_content")
	evidence, err := loadServiceQueryEvidence(ctx, content, repoID, serviceName)
	timer.Done(ctx,
		slog.Int("hostname_count", len(evidence.Hostnames)),
		slog.Int("environment_count", len(evidence.Environments)),
	)
	if err != nil {
		return fmt.Errorf("load service query evidence: %w", err)
	}

	// Load framework-detected routes from fact_records when ContentReader
	// is available (it has access to the same Postgres database).
	if content != nil {
		timer = startServiceQueryStage(ctx, opts.Logger, operation, serviceName, repoID, "framework_routes")
		frameworkRoutes, err := content.ListFrameworkRoutes(ctx, repoID)
		timer.Done(ctx, slog.Int("row_count", len(frameworkRoutes)))
		if err != nil {
			return fmt.Errorf("load framework routes: %w", err)
		}
		evidence.FrameworkRoutes = frameworkRoutes
	}

	if hostnames := buildServiceHostnameRows(evidence.Hostnames); len(hostnames) > 0 {
		workloadContext["hostnames"] = hostnames
	}
	if entrypoints := buildServiceEntrypoints(workloadContext, evidence); len(entrypoints) > 0 {
		workloadContext["entrypoints"] = entrypoints
	}

	instanceEnvironments, _ := workloadContext["instances"].([]map[string]any)
	observedEnvironments := mergeStringSets(
		distinctSortedInstanceField(instanceEnvironments, "environment"),
		serviceEvidenceEnvironmentNames(evidence.Environments),
	)
	if len(observedEnvironments) > 0 {
		workloadContext["observed_config_environments"] = observedEnvironments
	}

	if apiSurface := buildServiceAPISurface(evidence); len(apiSurface) > 0 && len(mapValue(workloadContext, "api_surface")) == 0 {
		workloadContext["api_surface"] = apiSurface
	}
	if networkPaths := buildServiceNetworkPaths(workloadContext, mapSliceValue(workloadContext, "entrypoints")); len(networkPaths) > 0 {
		workloadContext["network_paths"] = networkPaths
	}

	if graph != nil {
		hostnames := serviceEvidenceHostnames(evidence)
		traceLimit := boundedTraceEnrichmentLimit(opts.MaxDepth)
		if !opts.DirectOnly {
			timer = startServiceQueryStage(ctx, opts.Logger, operation, serviceName, repoID, "graph_dependents")
			dependentCandidates, err := queryProvisioningRepositoryCandidates(ctx, graph, repoID, traceLimit)
			timer.Done(ctx, slog.Int("row_count", len(dependentCandidates)))
			if err != nil {
				return fmt.Errorf("load graph dependents: %w", err)
			}
			if dependents := buildGraphDependents(dependentCandidates); len(dependents) > 0 {
				workloadContext["dependents"] = dependents
			}

			timer = startServiceQueryStage(ctx, opts.Logger, operation, serviceName, repoID, "consumer_repository_enrichment")
			consumers, err := loadConsumerRepositoryEnrichmentWithLimit(ctx, graph, content, repoID, serviceName, hostnames, traceLimit)
			timer.Done(ctx, slog.Int("row_count", len(consumers)))
			if err != nil {
				return fmt.Errorf("load consumer repository enrichment: %w", err)
			}
			if len(consumers) > 0 {
				workloadContext["consumer_repositories"] = consumers
			}
		}

		if opts.IncludeRelatedModuleUsage {
			timer = startServiceQueryStage(ctx, opts.Logger, operation, serviceName, repoID, "provisioning_source_chains")
			provisioningChains, err := loadProvisioningSourceChainsWithLimit(ctx, graph, content, repoID, traceLimit)
			timer.Done(ctx, slog.Int("row_count", len(provisioningChains)))
			if err != nil {
				return fmt.Errorf("load provisioning source chains: %w", err)
			}
			if len(provisioningChains) > 0 {
				workloadContext["provisioning_source_chains"] = provisioningChains
			}
		}
	}

	timer = startServiceQueryStage(ctx, opts.Logger, operation, serviceName, repoID, "documentation_overview")
	if documentationOverview := buildServiceDocumentationOverview(ctx, graph, workloadContext, evidence); len(documentationOverview) > 0 {
		workloadContext["documentation_overview"] = documentationOverview
	}
	timer.Done(ctx, slog.Bool("has_result", len(mapValue(workloadContext, "documentation_overview")) > 0))
	timer = startServiceQueryStage(ctx, opts.Logger, operation, serviceName, repoID, "deployment_evidence")
	deploymentEvidence, err := loadServiceDeploymentEvidence(ctx, graph, content, workloadContext)
	timer.Done(ctx, slog.Bool("has_result", len(deploymentEvidence) > 0))
	if err != nil {
		return fmt.Errorf("load service deployment evidence: %w", err)
	}
	if len(deploymentEvidence) > 0 {
		if graphEvidence := mapValue(workloadContext, "deployment_evidence"); len(graphEvidence) > 0 {
			deploymentEvidence = mergeServiceDeploymentEvidence(deploymentEvidence, graphEvidence)
		}
		workloadContext["deployment_evidence"] = deploymentEvidence
	}
	if supportOverview := buildServiceSupportOverview(workloadContext); len(supportOverview) > 0 {
		workloadContext["support_overview"] = supportOverview
	}
	timer = startServiceQueryStage(ctx, opts.Logger, operation, serviceName, repoID, "overview_assembly")
	workloadContext["deployment_overview"] = buildServiceDeploymentOverview(workloadContext)
	workloadContext["story_sections"] = buildServiceStorySections(workloadContext)
	timer.Done(ctx)

	return nil
}

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
