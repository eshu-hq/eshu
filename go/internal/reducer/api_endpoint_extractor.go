package reducer

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"gopkg.in/yaml.v3"
)

var openAPIHTTPMethodNames = map[string]struct{}{
	"get":     {},
	"put":     {},
	"post":    {},
	"delete":  {},
	"options": {},
	"head":    {},
	"patch":   {},
	"trace":   {},
}

type apiSpecSourceFile struct {
	repoID       string
	relativePath string
	source       string
}

// extractAPIEndpointSignals extracts route evidence from file facts already
// loaded for workload candidate extraction, avoiding a second fact scan.
func extractAPIEndpointSignals(envelopes []facts.Envelope) map[string][]APIEndpointSignal {
	if len(envelopes) == 0 {
		return nil
	}

	sourcesByRepo := make(map[string]map[string]string)
	specFiles := make([]apiSpecSourceFile, 0)
	signalsByRepo := make(map[string][]APIEndpointSignal)

	for _, env := range envelopes {
		if env.FactKind != "file" {
			continue
		}
		repoID := payloadStr(env.Payload, "repo_id")
		relativePath := payloadStr(env.Payload, "relative_path")
		if repoID == "" || relativePath == "" {
			continue
		}
		fileData, ok := env.Payload["parsed_file_data"].(map[string]any)
		if !ok {
			continue
		}
		source := strings.TrimSpace(payloadStr(fileData, "source"))
		if source != "" {
			if sourcesByRepo[repoID] == nil {
				sourcesByRepo[repoID] = make(map[string]string)
			}
			sourcesByRepo[repoID][relativePath] = source
			if isPotentialOpenAPISpecPath(relativePath) {
				specFiles = append(specFiles, apiSpecSourceFile{
					repoID:       repoID,
					relativePath: relativePath,
					source:       source,
				})
			}
		}
		if frameworkSignals := frameworkAPIEndpointSignals(relativePath, fileData); len(frameworkSignals) > 0 {
			signalsByRepo[repoID] = append(signalsByRepo[repoID], frameworkSignals...)
		}
	}

	for _, file := range specFiles {
		resolver := func(baseRelativePath, ref string) string {
			return sourcesByRepo[file.repoID][openAPIRefFilePath(baseRelativePath, ref)]
		}
		if specSignals := openAPIEndpointSignals(file.relativePath, file.source, resolver); len(specSignals) > 0 {
			signalsByRepo[file.repoID] = append(signalsByRepo[file.repoID], specSignals...)
		}
	}

	for repoID, signals := range signalsByRepo {
		signalsByRepo[repoID] = mergeAPIEndpointSignals(signals)
	}
	return signalsByRepo
}

// frameworkAPIEndpointSignals converts parser-owned framework_semantics into
// route signals without trying to infer routes from arbitrary source text.
func frameworkAPIEndpointSignals(relativePath string, fileData map[string]any) []APIEndpointSignal {
	semantics, ok := fileData["framework_semantics"].(map[string]any)
	if !ok {
		return nil
	}
	frameworks := toStringSlice(semantics["frameworks"])
	if len(frameworks) == 0 {
		return nil
	}

	var signals []APIEndpointSignal
	for _, framework := range frameworks {
		framework = strings.TrimSpace(framework)
		frameworkData, _ := semantics[framework].(map[string]any)
		if framework == "" || frameworkData == nil {
			continue
		}
		if framework == "nextjs" {
			if signal, ok := nextJSAPIEndpointSignal(relativePath, frameworkData); ok {
				signals = append(signals, signal)
			}
			continue
		}
		methods := normalizeHTTPMethods(toStringSlice(frameworkData["route_methods"]))
		for _, routePath := range toStringSlice(frameworkData["route_paths"]) {
			routePath = strings.TrimSpace(routePath)
			if routePath == "" {
				continue
			}
			signals = append(signals, APIEndpointSignal{
				Path:        routePath,
				Methods:     methods,
				SourceKinds: []string{"framework:" + framework},
				SourcePaths: []string{relativePath},
			})
		}
	}
	return signals
}

// nextJSAPIEndpointSignal translates parser-owned Next.js route module
// metadata into one URL path without guessing from arbitrary source text.
func nextJSAPIEndpointSignal(relativePath string, frameworkData map[string]any) (APIEndpointSignal, bool) {
	if apiStringValue(frameworkData["module_kind"]) != "route" {
		return APIEndpointSignal{}, false
	}
	routePath := nextJSRoutePath(toStringSlice(frameworkData["route_segments"]))
	if routePath == "" {
		return APIEndpointSignal{}, false
	}
	return APIEndpointSignal{
		Path:        routePath,
		Methods:     normalizeHTTPMethods(toStringSlice(frameworkData["route_verbs"])),
		SourceKinds: []string{"framework:nextjs"},
		SourcePaths: []string{relativePath},
	}, true
}

// nextJSRoutePath preserves parser-emitted route segment names while removing
// Next.js route-group folders that are not part of the public URL.
func nextJSRoutePath(segments []string) string {
	pathSegments := make([]string, 0, len(segments))
	for _, segment := range segments {
		segment = strings.TrimSpace(segment)
		if segment == "" || (strings.HasPrefix(segment, "(") && strings.HasSuffix(segment, ")")) {
			continue
		}
		pathSegments = append(pathSegments, segment)
	}
	if len(pathSegments) == 0 {
		return ""
	}
	return "/" + strings.Join(pathSegments, "/")
}

// openAPIEndpointSignals extracts endpoint paths from one OpenAPI root file
// and resolves the external path-file shapes used by split specifications.
func openAPIEndpointSignals(relativePath, source string, resolver func(baseRelativePath, ref string) string) []APIEndpointSignal {
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(source), &doc); err != nil {
		return nil
	}
	specVersion := apiStringValue(doc["openapi"])
	if specVersion == "" {
		specVersion = apiStringValue(doc["swagger"])
	}
	if specVersion == "" {
		return nil
	}
	resolveOpenAPIPathRefs(doc, relativePath, resolver)
	paths := apiMapValue(doc["paths"])
	if len(paths) == 0 {
		return nil
	}

	info := apiMapValue(doc["info"])
	apiVersion := apiStringValue(info["version"])
	signals := make([]APIEndpointSignal, 0, len(paths))
	for route, rawOperation := range paths {
		routeMap := apiMapValue(rawOperation)
		methods := make([]string, 0, len(routeMap))
		operationIDs := make([]string, 0, len(routeMap))
		for method, rawOperationSpec := range routeMap {
			method = strings.ToLower(strings.TrimSpace(method))
			if _, ok := openAPIHTTPMethodNames[method]; !ok {
				continue
			}
			methods = append(methods, method)
			operationMap := apiMapValue(rawOperationSpec)
			if operationID := apiStringValue(operationMap["operationId"]); operationID != "" {
				operationIDs = append(operationIDs, operationID)
			}
		}
		signals = append(signals, APIEndpointSignal{
			Path:         route,
			Methods:      uniqueSortedStrings(methods),
			OperationIDs: uniqueSortedStrings(operationIDs),
			SourceKinds:  []string{"openapi"},
			SourcePaths:  []string{relativePath},
			SpecVersions: []string{specVersion},
			APIVersions:  uniqueSortedStrings([]string{apiVersion}),
		})
	}
	sort.Slice(signals, func(i, j int) bool {
		return signals[i].Path < signals[j].Path
	})
	return signals
}

// resolveOpenAPIPathRefs resolves whole-path and per-path external refs. It
// intentionally stays bounded to the path-item level so reducer work does not
// become an unbounded spec traversal.
func resolveOpenAPIPathRefs(doc map[string]any, baseRelativePath string, resolver func(baseRelativePath, ref string) string) {
	if resolver == nil {
		return
	}
	paths := apiMapValue(doc["paths"])
	if len(paths) == 0 {
		return
	}
	if ref := apiStringValue(paths["$ref"]); ref != "" && len(paths) == 1 {
		content := resolver(baseRelativePath, ref)
		if content == "" {
			return
		}
		var resolved map[string]any
		if err := yaml.Unmarshal([]byte(content), &resolved); err != nil {
			return
		}
		doc["paths"] = resolved
		resolveOpenAPIPathItemRefs(resolved, openAPIRefFilePath(baseRelativePath, ref), resolver)
		return
	}
	resolveOpenAPIPathItemRefs(paths, baseRelativePath, resolver)
}

// resolveOpenAPIPathItemRefs replaces path-item refs with the referenced
// method map, preserving the route key from the parent paths object.
func resolveOpenAPIPathItemRefs(paths map[string]any, baseRelativePath string, resolver func(baseRelativePath, ref string) string) {
	for route, rawPathItem := range paths {
		pathItemMap := apiMapValue(rawPathItem)
		ref := apiStringValue(pathItemMap["$ref"])
		if ref == "" {
			continue
		}
		content := resolver(baseRelativePath, ref)
		if content == "" {
			continue
		}
		var resolved map[string]any
		if err := yaml.Unmarshal([]byte(content), &resolved); err != nil {
			continue
		}
		paths[route] = resolved
	}
}

// mergeAPIEndpointSignals keeps one endpoint signal per path while retaining
// provenance from every framework or spec file that described that path.
func mergeAPIEndpointSignals(signals []APIEndpointSignal) []APIEndpointSignal {
	if len(signals) == 0 {
		return nil
	}
	indexByPath := make(map[string]int, len(signals))
	merged := make([]APIEndpointSignal, 0, len(signals))
	for _, signal := range signals {
		path := strings.TrimSpace(signal.Path)
		if path == "" {
			continue
		}
		if index, ok := indexByPath[path]; ok {
			existing := merged[index]
			existing.Methods = uniqueSortedStrings(append(existing.Methods, signal.Methods...))
			existing.OperationIDs = uniqueSortedStrings(append(existing.OperationIDs, signal.OperationIDs...))
			existing.SourceKinds = uniqueSortedStrings(append(existing.SourceKinds, signal.SourceKinds...))
			existing.SourcePaths = uniqueSortedStrings(append(existing.SourcePaths, signal.SourcePaths...))
			existing.SpecVersions = uniqueSortedStrings(append(existing.SpecVersions, signal.SpecVersions...))
			existing.APIVersions = uniqueSortedStrings(append(existing.APIVersions, signal.APIVersions...))
			merged[index] = existing
			continue
		}
		signal.Path = path
		signal.Methods = uniqueSortedStrings(signal.Methods)
		signal.OperationIDs = uniqueSortedStrings(signal.OperationIDs)
		signal.SourceKinds = uniqueSortedStrings(signal.SourceKinds)
		signal.SourcePaths = uniqueSortedStrings(signal.SourcePaths)
		signal.SpecVersions = uniqueSortedStrings(signal.SpecVersions)
		signal.APIVersions = uniqueSortedStrings(signal.APIVersions)
		indexByPath[path] = len(merged)
		merged = append(merged, signal)
	}
	sort.Slice(merged, func(i, j int) bool {
		return merged[i].Path < merged[j].Path
	})
	return merged
}

// normalizeHTTPMethods stores methods in the same lowercase form used by the
// OpenAPI path extractor and API query surface.
func normalizeHTTPMethods(methods []string) []string {
	normalized := make([]string, 0, len(methods))
	for _, method := range methods {
		method = strings.ToLower(strings.TrimSpace(method))
		if method == "" {
			continue
		}
		normalized = append(normalized, method)
	}
	return uniqueSortedStrings(normalized)
}

// isPotentialOpenAPISpecPath mirrors the query evidence filter so reducer and
// read surfaces consider the same likely spec roots.
func isPotentialOpenAPISpecPath(relativePath string) bool {
	lower := strings.ToLower(relativePath)
	return strings.Contains(lower, "openapi") ||
		strings.Contains(lower, "swagger") ||
		strings.Contains(lower, "spec")
}

// openAPIRefFilePath resolves a file-local OpenAPI ref and strips JSON pointer
// fragments before looking up sibling file facts.
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
	return filepath.Clean(filepath.Join(filepath.Dir(baseRelativePath), ref))
}

func apiMapValue(raw any) map[string]any {
	typed, _ := raw.(map[string]any)
	return typed
}

func apiStringValue(raw any) string {
	value, _ := raw.(string)
	return strings.TrimSpace(value)
}
