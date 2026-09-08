// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Service evidence parsing holds the pure content extractors shared by the
// service evidence stayer in package query and the repository narrative
// overviews in package repository: docs-route references, OpenAPI spec
// summaries (including `$ref` resolution through a caller-supplied resolver),
// and the loose YAML/JSON document accessors they are built from.
//
// The implementation moved here from querycontract for #6060 lane-B B3
// review: querycontract stays dependency-neutral (Go standard library only),
// so the `gopkg.in/yaml.v3` runtime lives in this leaf instead. Package
// query keeps thin wrappers so existing callers are unchanged.

package serviceevidence

import (
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// ServiceSliceValue, ServiceMapValue, and ServiceStringValue read one field
// out of a loosely-parsed YAML/JSON spec document. A spec file is caller
// content, so any node can be any type; each accessor returns the zero value
// rather than panicking on a shape the repository happened to commit.
func ServiceSliceValue(raw any) []any {
	switch typed := raw.(type) {
	case []any:
		return typed
	default:
		return nil
	}
}

// ServiceMapValue reads a map field out of a loosely-parsed spec document,
// returning nil when the node is not a map.
func ServiceMapValue(raw any) map[string]any {
	typed, _ := raw.(map[string]any)
	return typed
}

// ServiceStringValue reads a string field out of a loosely-parsed spec
// document, returning "" when the node is not a string.
func ServiceStringValue(raw any) string {
	value, _ := raw.(string)
	return value
}

// OpenAPIRefFilePath resolves a `$ref` against the directory of the spec file
// that carried it, dropping any `#/...` JSON-pointer fragment. It returns a
// repository-relative path, or an empty string when the ref is only a fragment.
func OpenAPIRefFilePath(baseRelativePath, ref string) string {
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

var serviceDocsRoutePattern = regexp.MustCompile(`(?i)['"](/[^'"]+)['"]`)

// ExtractDocsRoutes returns the sorted, de-duplicated docs-like route
// references quoted in content.
func ExtractDocsRoutes(content string) []string {
	matches := serviceDocsRoutePattern.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	routes := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		route := strings.TrimSpace(match[1])
		if route == "" {
			continue
		}
		if !LooksLikeDocsRoute(route) {
			continue
		}
		if _, ok := seen[route]; ok {
			continue
		}
		seen[route] = struct{}{}
		routes = append(routes, route)
	}
	sort.Strings(routes)
	return routes
}

// LooksLikeDocsRoute reports whether a route reference smells like
// documentation: it names docs/swagger/openapi, or carries a spec/specs path
// segment.
func LooksLikeDocsRoute(route string) bool {
	lower := strings.ToLower(route)
	if strings.Contains(lower, "docs") || strings.Contains(lower, "swagger") || strings.Contains(lower, "openapi") {
		return true
	}
	for _, segment := range strings.FieldsFunc(lower, func(r rune) bool {
		switch r {
		case '/', '_', '-', '.', ':':
			return true
		default:
			return false
		}
	}) {
		if segment == "spec" || segment == "specs" {
			return true
		}
	}
	return false
}

var openAPIMethodNames = map[string]struct{}{
	"get": {}, "put": {}, "post": {}, "delete": {}, "patch": {}, "options": {}, "head": {}, "trace": {},
}

// ExtractAPISpecEvidence summarizes one candidate API spec file, resolving
// external `$ref` path entries through resolver first.
//
// The returned error is a resolver read failure and nothing else. Until #5720
// round 10 the resolver reported a read error and a genuinely absent file as
// the same empty string, so a transient Postgres error resolving
// `./paths/index.yaml` silently produced a spec with fewer `servers` entries,
// fewer derived hostnames, fewer cross-repository consumer searches, and
// `consumer_repositories_truncated` FALSE -- a complete-looking answer built
// from a failure rather than a bound. loadServiceQueryEvidence already
// propagates the identical error from the same reader method when it hydrates
// a listing row; this call site now matches it.
//
// A referenced file that parses badly is still tolerated: malformed YAML in a
// referenced spec is a property of the repository's own content, which every
// other extractor here also reads on a best-effort basis, not a failure of
// the read.
func ExtractAPISpecEvidence(file querycontract.FileContent, resolver querycontract.SpecFileResolver) (querycontract.ServiceAPISpecEvidence, bool, error) {
	format := serviceEvidenceFormat(file.RelativePath)
	if !isPotentialAPISpecPath(file.RelativePath) {
		return querycontract.ServiceAPISpecEvidence{}, false, nil
	}

	doc, err := parseLooseYAMLDocument(file.Content)
	if err == nil {
		if resolveErr := resolveOpenAPIPathRefs(doc, file.RelativePath, resolver); resolveErr != nil {
			return querycontract.ServiceAPISpecEvidence{}, false, resolveErr
		}
		if spec, ok := buildOpenAPISpecEvidence(file.RelativePath, format, doc); ok {
			return spec, true, nil
		}
	}

	return querycontract.ServiceAPISpecEvidence{
		RelativePath: file.RelativePath,
		Format:       format,
		Parsed:       false,
	}, true, nil
}

// ExtractAPISpecEvidenceWithoutRefs is ExtractAPISpecEvidence for callers that
// hold no reader and therefore pass no resolver. resolveOpenAPIPathRefs
// returns nil on a nil resolver before it can read anything, so the dropped
// error is structurally always nil rather than a swallowed failure.
func ExtractAPISpecEvidenceWithoutRefs(file querycontract.FileContent) (querycontract.ServiceAPISpecEvidence, bool) {
	spec, ok, _ := ExtractAPISpecEvidence(file, nil)
	return spec, ok
}

// resolveOpenAPIPathRefs resolves $ref entries in the paths object of an
// OpenAPI document. It handles two patterns:
//
//  1. Whole-paths $ref: paths: { $ref: './paths/index.yaml' }
//  2. Per-path-item $ref: paths: { /route: { $ref: './paths/route.yaml' } }
//
// A nil resolver means the caller holds no reader; it returns nil before any
// read, which is what makes ExtractAPISpecEvidenceWithoutRefs error-free.
func resolveOpenAPIPathRefs(doc map[string]any, baseRelativePath string, resolver querycontract.SpecFileResolver) error {
	if resolver == nil {
		return nil
	}
	paths := ServiceMapValue(doc["paths"])
	if len(paths) == 0 {
		return nil
	}

	// Case 1: whole-paths $ref — the paths map itself contains only a $ref key.
	if ref, ok := paths["$ref"].(string); ok && len(paths) == 1 {
		content, err := resolver(baseRelativePath, ref)
		if err != nil {
			return err
		}
		if content == "" {
			return nil
		}
		resolved, parseErr := parseLooseYAMLDocument(content)
		if parseErr != nil {
			return nil
		}
		doc["paths"] = resolved
		return resolveOpenAPIPathItemRefs(resolved, OpenAPIRefFilePath(baseRelativePath, ref), resolver)
	}

	// Case 2: per-path-item $ref — individual path items reference external files.
	return resolveOpenAPIPathItemRefs(paths, baseRelativePath, resolver)
}

func resolveOpenAPIPathItemRefs(paths map[string]any, baseRelativePath string, resolver querycontract.SpecFileResolver) error {
	for route, rawPathItem := range paths {
		pathItemMap := ServiceMapValue(rawPathItem)
		if pathItemMap == nil {
			continue
		}
		ref, ok := pathItemMap["$ref"].(string)
		if !ok || ref == "" {
			continue
		}
		content, err := resolver(baseRelativePath, ref)
		if err != nil {
			return err
		}
		if content == "" {
			continue
		}
		resolved, parseErr := parseLooseYAMLDocument(content)
		if parseErr != nil {
			continue
		}
		paths[route] = resolved
	}
	return nil
}

func buildOpenAPISpecEvidence(relativePath string, format string, doc map[string]any) (querycontract.ServiceAPISpecEvidence, bool) {
	specVersion := ServiceStringValue(doc["openapi"])
	if specVersion == "" {
		specVersion = ServiceStringValue(doc["swagger"])
	}

	paths := ServiceMapValue(doc["paths"])
	if specVersion == "" && len(paths) == 0 {
		return querycontract.ServiceAPISpecEvidence{}, false
	}

	operationIDCount := 0
	methodCount := 0
	docsRoutes := make([]string, 0)
	endpoints := make([]querycontract.ServiceAPIEndpointEvidence, 0, len(paths))
	for route, rawOperation := range paths {
		routeMap := ServiceMapValue(rawOperation)
		methods := make([]string, 0, len(routeMap))
		operationIDs := make([]string, 0, len(routeMap))
		for method, rawOperationSpec := range routeMap {
			if _, ok := openAPIMethodNames[strings.ToLower(method)]; !ok {
				continue
			}
			methodCount++
			methods = append(methods, strings.ToLower(method))
			operationMap := ServiceMapValue(rawOperationSpec)
			if operationID := ServiceStringValue(operationMap["operationId"]); operationID != "" {
				operationIDCount++
				operationIDs = append(operationIDs, operationID)
			}
		}
		sort.Strings(methods)
		sort.Strings(operationIDs)
		endpoints = append(endpoints, querycontract.ServiceAPIEndpointEvidence{
			Path:         route,
			Methods:      methods,
			OperationIDs: operationIDs,
		})
		if LooksLikeDocsRoute(route) {
			docsRoutes = append(docsRoutes, route)
		}
	}
	sort.Strings(docsRoutes)
	sort.Slice(endpoints, func(i, j int) bool {
		return endpoints[i].Path < endpoints[j].Path
	})

	hostnames := make([]string, 0)
	seenHostnames := map[string]struct{}{}
	for _, server := range ServiceSliceValue(doc["servers"]) {
		serverMap := ServiceMapValue(server)
		serverURL := ServiceStringValue(serverMap["url"])
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

	info := ServiceMapValue(doc["info"])
	return querycontract.ServiceAPISpecEvidence{
		RelativePath:     relativePath,
		Format:           format,
		Parsed:           true,
		SpecVersion:      specVersion,
		APIVersion:       ServiceStringValue(info["version"]),
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
