// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type serviceEvidenceReader interface {
	ListRepoFiles(ctx context.Context, repoID string, limit int) ([]FileContent, error)
	GetFileContent(ctx context.Context, repoID, relativePath string) (*FileContent, error)
}

const serviceEvidenceFileLimit = 5000

var openAPIMethodNames = map[string]struct{}{
	"get": {}, "put": {}, "post": {}, "delete": {}, "patch": {}, "options": {}, "head": {}, "trace": {},
}

func loadServiceQueryEvidence(
	ctx context.Context,
	reader serviceEvidenceReader,
	repoID string,
	serviceName string,
) (ServiceQueryEvidence, error) {
	if reader == nil || repoID == "" {
		return ServiceQueryEvidence{}, nil
	}

	files, err := reader.ListRepoFiles(ctx, repoID, serviceEvidenceFileLimit)
	if err != nil {
		return ServiceQueryEvidence{}, fmt.Errorf("list service evidence files: %w", err)
	}

	var evidence ServiceQueryEvidence
	seenHostnames := map[string]struct{}{}
	seenEntrypointCandidates := map[string]struct{}{}
	seenEnvironments := map[string]struct{}{}
	seenDocsRoutes := map[string]struct{}{}
	seenSpecs := map[string]struct{}{}
	normalizedServiceName := normalizeEvidenceToken(serviceName)

	for _, file := range files {
		if !isServiceEvidenceCandidate(file, normalizedServiceName) {
			continue
		}

		hydrated := file
		if strings.TrimSpace(hydrated.Content) == "" {
			fileContent, err := reader.GetFileContent(ctx, repoID, file.RelativePath)
			if err != nil {
				return ServiceQueryEvidence{}, fmt.Errorf("get service evidence file %q: %w", file.RelativePath, err)
			}
			if fileContent == nil {
				continue
			}
			hydrated = *fileContent
		}

		hostnameCandidates := extractObservedHostnameCandidates(hydrated.Content)
		hostnames := exactObservedHostnameCandidates(hostnameCandidates)
		environments := inferObservedEnvironments(hydrated.RelativePath, hydrated.Content, hostnames)
		for _, candidate := range hostnameCandidates {
			if candidate.Classification == "exact_hostname" {
				continue
			}
			key := candidate.Value + "\x00" + candidate.Classification + "\x00" + hydrated.RelativePath
			if _, ok := seenEntrypointCandidates[key]; ok {
				continue
			}
			seenEntrypointCandidates[key] = struct{}{}
			evidence.EntrypointCandidates = append(evidence.EntrypointCandidates, ServiceEntrypointCandidateEvidence{
				Candidate:      candidate.Value,
				Classification: candidate.Classification,
				RelativePath:   hydrated.RelativePath,
				Reason:         candidate.Reason,
			})
		}
		for _, hostname := range hostnames {
			environment := inferHostnameEnvironment(hostname)
			if environment == "" && len(environments) > 0 {
				environment = environments[0]
			}
			if _, ok := seenHostnames[hostname]; ok {
				continue
			}
			seenHostnames[hostname] = struct{}{}
			evidence.Hostnames = append(evidence.Hostnames, ServiceHostnameEvidence{
				Hostname:     hostname,
				Environment:  environment,
				RelativePath: hydrated.RelativePath,
				Reason:       exactHostnameCandidateReason(hostnameCandidates, hostname),
			})
		}

		for _, environment := range environments {
			if _, ok := seenEnvironments[environment]; ok {
				continue
			}
			seenEnvironments[environment] = struct{}{}
			evidence.Environments = append(evidence.Environments, ServiceEnvironmentEvidence{
				Environment:  environment,
				RelativePath: hydrated.RelativePath,
				Reason:       "path_or_content_environment_signal",
			})
		}

		for _, route := range extractDocsRoutes(hydrated.Content) {
			if _, ok := seenDocsRoutes[route]; ok {
				continue
			}
			seenDocsRoutes[route] = struct{}{}
			evidence.DocsRoutes = append(evidence.DocsRoutes, ServiceDocsRouteEvidence{
				Route:        route,
				RelativePath: hydrated.RelativePath,
				Reason:       "docs_route_reference",
			})
		}

		if spec, ok := extractAPISpecEvidence(hydrated, buildSpecFileResolver(reader, ctx, repoID)); ok {
			key := spec.RelativePath
			if _, ok := seenSpecs[key]; ok {
				continue
			}
			seenSpecs[key] = struct{}{}
			evidence.APISpecs = append(evidence.APISpecs, spec)
		}
	}

	sort.Slice(evidence.Hostnames, func(i, j int) bool {
		if evidence.Hostnames[i].Hostname != evidence.Hostnames[j].Hostname {
			return evidence.Hostnames[i].Hostname < evidence.Hostnames[j].Hostname
		}
		return evidence.Hostnames[i].RelativePath < evidence.Hostnames[j].RelativePath
	})
	sort.Slice(evidence.Environments, func(i, j int) bool {
		if evidence.Environments[i].Environment != evidence.Environments[j].Environment {
			return evidence.Environments[i].Environment < evidence.Environments[j].Environment
		}
		return evidence.Environments[i].RelativePath < evidence.Environments[j].RelativePath
	})
	sort.Slice(evidence.DocsRoutes, func(i, j int) bool {
		if evidence.DocsRoutes[i].Route != evidence.DocsRoutes[j].Route {
			return evidence.DocsRoutes[i].Route < evidence.DocsRoutes[j].Route
		}
		return evidence.DocsRoutes[i].RelativePath < evidence.DocsRoutes[j].RelativePath
	})
	sort.Slice(evidence.EntrypointCandidates, func(i, j int) bool {
		if evidence.EntrypointCandidates[i].Candidate != evidence.EntrypointCandidates[j].Candidate {
			return evidence.EntrypointCandidates[i].Candidate < evidence.EntrypointCandidates[j].Candidate
		}
		if evidence.EntrypointCandidates[i].Classification != evidence.EntrypointCandidates[j].Classification {
			return evidence.EntrypointCandidates[i].Classification < evidence.EntrypointCandidates[j].Classification
		}
		return evidence.EntrypointCandidates[i].RelativePath < evidence.EntrypointCandidates[j].RelativePath
	})
	sort.Slice(evidence.APISpecs, func(i, j int) bool {
		return evidence.APISpecs[i].RelativePath < evidence.APISpecs[j].RelativePath
	})

	return evidence, nil
}

func isServiceEvidenceCandidate(file FileContent, normalizedServiceName string) bool {
	path := strings.ToLower(file.RelativePath)
	if path == "" {
		return false
	}
	if normalizedServiceName != "" && strings.Contains(normalizeEvidenceToken(path), normalizedServiceName) {
		return true
	}

	switch filepath.Ext(path) {
	case ".yaml", ".yml", ".json", ".js", ".mjs", ".cjs", ".ts", ".mts", ".cts", ".md":
	default:
		return false
	}

	for _, keyword := range []string{
		"openapi", "swagger", "spec", "docs", "route", "server", "ingress",
		"gateway", "deploy", "values", "config", "application",
	} {
		if strings.Contains(path, keyword) {
			return true
		}
	}
	return false
}

// specFileResolver resolves a relative $ref path from a base spec file and
// returns the raw content of the referenced file. Returns empty string when
// the reference cannot be resolved.
type specFileResolver func(baseRelativePath, ref string) string

func extractAPISpecEvidence(file FileContent, resolver specFileResolver) (ServiceAPISpecEvidence, bool) {
	format := serviceEvidenceFormat(file.RelativePath)
	if !isPotentialAPISpecPath(file.RelativePath) {
		return ServiceAPISpecEvidence{}, false
	}

	doc, err := parseLooseYAMLDocument(file.Content)
	if err == nil {
		resolveOpenAPIPathRefs(doc, file.RelativePath, resolver)
		if spec, ok := buildOpenAPISpecEvidence(file.RelativePath, format, doc); ok {
			return spec, true
		}
	}

	return ServiceAPISpecEvidence{
		RelativePath: file.RelativePath,
		Format:       format,
		Parsed:       false,
	}, true
}

// resolveOpenAPIPathRefs resolves $ref entries in the paths object of an
// OpenAPI document. It handles two patterns:
//  1. Whole-paths $ref: paths: { $ref: './paths/index.yaml' }
//  2. Per-path-item $ref: paths: { /route: { $ref: './paths/route.yaml' } }
func resolveOpenAPIPathRefs(doc map[string]any, baseRelativePath string, resolver specFileResolver) {
	if resolver == nil {
		return
	}
	paths := serviceMapValue(doc["paths"])
	if len(paths) == 0 {
		return
	}

	// Case 1: whole-paths $ref — the paths map itself contains only a $ref key.
	if ref, ok := paths["$ref"].(string); ok && len(paths) == 1 {
		content := resolver(baseRelativePath, ref)
		if content == "" {
			return
		}
		resolved, err := parseLooseYAMLDocument(content)
		if err != nil {
			return
		}
		doc["paths"] = resolved
		resolveOpenAPIPathItemRefs(resolved, openAPIRefFilePath(baseRelativePath, ref), resolver)
		return
	}

	// Case 2: per-path-item $ref — individual path items reference external files.
	resolveOpenAPIPathItemRefs(paths, baseRelativePath, resolver)
}

func resolveOpenAPIPathItemRefs(paths map[string]any, baseRelativePath string, resolver specFileResolver) {
	for route, rawPathItem := range paths {
		pathItemMap := serviceMapValue(rawPathItem)
		if pathItemMap == nil {
			continue
		}
		ref, ok := pathItemMap["$ref"].(string)
		if !ok || ref == "" {
			continue
		}
		content := resolver(baseRelativePath, ref)
		if content == "" {
			continue
		}
		resolved, err := parseLooseYAMLDocument(content)
		if err != nil {
			continue
		}
		paths[route] = resolved
	}
}

func buildOpenAPISpecEvidence(relativePath string, format string, doc map[string]any) (ServiceAPISpecEvidence, bool) {
	specVersion := serviceStringValue(doc["openapi"])
	if specVersion == "" {
		specVersion = serviceStringValue(doc["swagger"])
	}

	paths := serviceMapValue(doc["paths"])
	if specVersion == "" && len(paths) == 0 {
		return ServiceAPISpecEvidence{}, false
	}

	operationIDCount := 0
	methodCount := 0
	docsRoutes := make([]string, 0)
	endpoints := make([]ServiceAPIEndpointEvidence, 0, len(paths))
	for route, rawOperation := range paths {
		routeMap := serviceMapValue(rawOperation)
		methods := make([]string, 0, len(routeMap))
		operationIDs := make([]string, 0, len(routeMap))
		for method, rawOperationSpec := range routeMap {
			if _, ok := openAPIMethodNames[strings.ToLower(method)]; !ok {
				continue
			}
			methodCount++
			methods = append(methods, strings.ToLower(method))
			operationMap := serviceMapValue(rawOperationSpec)
			if operationID := serviceStringValue(operationMap["operationId"]); operationID != "" {
				operationIDCount++
				operationIDs = append(operationIDs, operationID)
			}
		}
		sort.Strings(methods)
		sort.Strings(operationIDs)
		endpoints = append(endpoints, ServiceAPIEndpointEvidence{
			Path:         route,
			Methods:      methods,
			OperationIDs: operationIDs,
		})
		if looksLikeDocsRoute(route) {
			docsRoutes = append(docsRoutes, route)
		}
	}
	sort.Strings(docsRoutes)
	sort.Slice(endpoints, func(i, j int) bool {
		return endpoints[i].Path < endpoints[j].Path
	})

	hostnames := make([]string, 0)
	seenHostnames := map[string]struct{}{}
	for _, server := range serviceSliceValue(doc["servers"]) {
		serverMap := serviceMapValue(server)
		serverURL := serviceStringValue(serverMap["url"])
		if serverURL == "" {
			continue
		}
		hostname := hostnameFromURL(serverURL)
		if hostname == "" {
			continue
		}
		if _, ok := seenHostnames[hostname]; ok {
			continue
		}
		seenHostnames[hostname] = struct{}{}
		hostnames = append(hostnames, hostname)
	}
	sort.Strings(hostnames)

	info := serviceMapValue(doc["info"])
	return ServiceAPISpecEvidence{
		RelativePath:     relativePath,
		Format:           format,
		Parsed:           true,
		SpecVersion:      specVersion,
		APIVersion:       serviceStringValue(info["version"]),
		EndpointCount:    len(paths),
		MethodCount:      methodCount,
		OperationIDCount: operationIDCount,
		DocsRoutes:       docsRoutes,
		Hostnames:        hostnames,
		Endpoints:        endpoints,
	}, true
}

func parseLooseYAMLDocument(content string) (map[string]any, error) {
	var document map[string]any
	if err := yaml.Unmarshal([]byte(content), &document); err != nil {
		return nil, err
	}
	return document, nil
}

// buildSpecFileResolver creates a specFileResolver closure that reads
// referenced files via the serviceEvidenceReader.
func buildSpecFileResolver(reader serviceEvidenceReader, ctx context.Context, repoID string) specFileResolver {
	return func(baseRelativePath, ref string) string {
		if reader == nil || ref == "" {
			return ""
		}
		// Resolve relative path against the base spec file's directory.
		resolved := openAPIRefFilePath(baseRelativePath, ref)

		fc, err := reader.GetFileContent(ctx, repoID, resolved)
		if err != nil || fc == nil {
			return ""
		}
		return fc.Content
	}
}

func openAPIRefFilePath(baseRelativePath, ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	if fragmentIndex := strings.Index(ref, "#"); fragmentIndex >= 0 {
		ref = ref[:fragmentIndex]
	}
	if ref == "" {
		return ""
	}
	baseDir := filepath.Dir(baseRelativePath)
	return filepath.Clean(filepath.Join(baseDir, ref))
}

func serviceEvidenceFormat(relativePath string) string {
	switch strings.ToLower(filepath.Ext(relativePath)) {
	case ".yaml", ".yml":
		return "yaml"
	case ".json":
		return "json"
	case ".js", ".mjs", ".cjs":
		return "javascript"
	case ".ts", ".mts", ".cts":
		return "typescript"
	case ".md":
		return "markdown"
	default:
		return "text"
	}
}

func isPotentialAPISpecPath(relativePath string) bool {
	lower := strings.ToLower(relativePath)
	return strings.Contains(lower, "openapi") ||
		strings.Contains(lower, "swagger") ||
		strings.Contains(lower, "spec")
}

func hostnameFromURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}

func normalizeEvidenceToken(text string) string {
	lower := strings.ToLower(text)
	replacer := strings.NewReplacer(
		"/", "_",
		".", "_",
		"-", "_",
		":", "_",
		"@", "_",
		"\n", "_",
		"\t", "_",
		" ", "_",
	)
	return "_" + replacer.Replace(lower) + "_"
}

func serviceSliceValue(raw any) []any {
	switch typed := raw.(type) {
	case []any:
		return typed
	default:
		return nil
	}
}

func serviceMapValue(raw any) map[string]any {
	typed, _ := raw.(map[string]any)
	return typed
}

func serviceStringValue(raw any) string {
	value, _ := raw.(string)
	return value
}
