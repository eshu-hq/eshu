package parser

import (
	"fmt"
	"sort"
	"strings"
)

func isKustomization(apiVersion string, kind string, filename string) bool {
	if strings.HasPrefix(apiVersion, "kustomize.config.k8s.io/") {
		return true
	}
	if kind == "Kustomization" && strings.HasPrefix(apiVersion, "kustomize") {
		return true
	}
	lower := strings.ToLower(filename)
	return lower == "kustomization.yaml" || lower == "kustomization.yml"
}

func parseKustomization(document map[string]any, path string, lineNumber int) map[string]any {
	bases := collectKustomizeBaseRefs(document)
	return map[string]any{
		"name":          "kustomization",
		"line_number":   lineNumber,
		"namespace":     strings.TrimSpace(fmt.Sprint(document["namespace"])),
		"resources":     document["resources"],
		"bases":         bases,
		"resource_refs": collectKustomizeResourceRefs(document, bases),
		"helm_refs":     collectKustomizeObjectRefs(document, "helmCharts", "name", "repo", "releaseName"),
		"image_refs":    collectKustomizeObjectRefs(document, "images", "name", "newName"),
		"patches":       collectPatchPaths(document["patches"]),
		"patch_targets": collectPatchTargets(document["patches"]),
		"path":          path,
		"lang":          "yaml",
	}
}

func collectPatchPaths(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	paths := make([]string, 0, len(items))
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			continue
		}
		pathValue := strings.TrimSpace(fmt.Sprint(object["path"]))
		if pathValue != "" && pathValue != "<nil>" {
			paths = append(paths, pathValue)
		}
	}
	sort.Strings(paths)
	return paths
}

func collectPatchTargets(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	targets := make([]string, 0, len(items))
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			continue
		}
		target, ok := object["target"].(map[string]any)
		if !ok {
			continue
		}
		kind := strings.TrimSpace(fmt.Sprint(target["kind"]))
		name := strings.TrimSpace(fmt.Sprint(target["name"]))
		if kind == "" || kind == "<nil>" || name == "" || name == "<nil>" {
			continue
		}
		targets = append(targets, kind+"/"+name)
	}
	sort.Strings(targets)
	return dedupeNonEmptyStrings(targets)
}

func collectKustomizeBaseRefs(document map[string]any) []string {
	values := make([]string, 0)
	if bases, ok := document["bases"].([]any); ok {
		values = append(values, collectKustomizePathRefs(bases)...)
	}
	if resources, ok := document["resources"].([]any); ok {
		values = append(values, collectKustomizePathRefs(resources)...)
	}
	bases := dedupeNonEmptyStrings(values)
	sort.Strings(bases)
	return bases
}

func collectKustomizeResourceRefs(document map[string]any, bases []string) []string {
	baseSet := make(map[string]struct{}, len(bases))
	for _, base := range bases {
		baseSet[base] = struct{}{}
	}

	refs := make([]string, 0)
	for _, value := range append(
		collectKustomizeStringValues(document["resources"]),
		collectKustomizeStringValues(document["components"])...,
	) {
		if _, isBase := baseSet[value]; isBase {
			continue
		}
		refs = append(refs, value)
	}
	refs = dedupeNonEmptyStrings(refs)
	sort.Strings(refs)
	return refs
}

func collectKustomizeObjectRefs(document map[string]any, listKey string, fieldKeys ...string) []string {
	refs := make([]string, 0)
	items, ok := document[listKey].([]any)
	if !ok {
		return nil
	}
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			continue
		}
		for _, fieldKey := range fieldKeys {
			value := strings.TrimSpace(fmt.Sprint(object[fieldKey]))
			if value == "" || value == "<nil>" {
				continue
			}
			refs = append(refs, value)
		}
	}
	refs = dedupeNonEmptyStrings(refs)
	sort.Strings(refs)
	return refs
}

func collectKustomizePathRefs(values []any) []string {
	refs := make([]string, 0, len(values))
	for _, value := range values {
		path := strings.TrimSpace(fmt.Sprint(value))
		if path == "" || path == "<nil>" {
			continue
		}
		if isRemoteKustomizeRef(path) {
			continue
		}
		lower := strings.ToLower(path)
		if strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml") || strings.HasSuffix(lower, ".json") {
			continue
		}
		refs = append(refs, path)
	}
	return refs
}

func collectKustomizeStringValues(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	refs := make([]string, 0, len(items))
	for _, item := range items {
		path := strings.TrimSpace(fmt.Sprint(item))
		if path == "" || path == "<nil>" {
			continue
		}
		refs = append(refs, path)
	}
	return refs
}

func isRemoteKustomizeRef(value string) bool {
	return strings.Contains(value, "://")
}

func dedupeNonEmptyStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		cleaned := strings.TrimSpace(value)
		if cleaned == "" {
			continue
		}
		if _, ok := seen[cleaned]; ok {
			continue
		}
		seen[cleaned] = struct{}{}
		result = append(result, cleaned)
	}
	return result
}

func collectMetadataLabels(metadata map[string]any) string {
	labels, ok := metadata["labels"].(map[string]any)
	if !ok || len(labels) == 0 {
		return ""
	}
	keys := sortedMapKeysAny(labels)
	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		value := strings.TrimSpace(fmt.Sprint(labels[key]))
		if value == "" || value == "<nil>" {
			continue
		}
		pairs = append(pairs, key+"="+value)
	}
	return strings.Join(pairs, ",")
}

func sortedMapKeysAny(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func isCrossplaneXRD(apiVersion string, kind string) bool {
	return strings.HasPrefix(apiVersion, "apiextensions.crossplane.io/") && kind == "CompositeResourceDefinition"
}

func parseCrossplaneXRD(document map[string]any, metadata map[string]any, path string, lineNumber int) map[string]any {
	spec, _ := document["spec"].(map[string]any)
	names, _ := spec["names"].(map[string]any)
	claimNames, _ := spec["claimNames"].(map[string]any)
	return map[string]any{
		"name":         strings.TrimSpace(fmt.Sprint(metadata["name"])),
		"line_number":  lineNumber,
		"group":        strings.TrimSpace(fmt.Sprint(spec["group"])),
		"kind":         strings.TrimSpace(fmt.Sprint(names["kind"])),
		"plural":       strings.TrimSpace(fmt.Sprint(names["plural"])),
		"claim_kind":   strings.TrimSpace(fmt.Sprint(claimNames["kind"])),
		"claim_plural": strings.TrimSpace(fmt.Sprint(claimNames["plural"])),
		"path":         path,
		"lang":         "yaml",
	}
}

func isCrossplaneComposition(apiVersion string, kind string) bool {
	return strings.HasPrefix(apiVersion, "apiextensions.crossplane.io/") && kind == "Composition"
}

func parseCrossplaneComposition(document map[string]any, metadata map[string]any, path string, lineNumber int) map[string]any {
	spec, _ := document["spec"].(map[string]any)
	compositeRef, _ := spec["compositeTypeRef"].(map[string]any)
	resourceNames := make([]string, 0)
	if resources, ok := spec["resources"].([]any); ok {
		for _, item := range resources {
			resource, ok := item.(map[string]any)
			if !ok {
				continue
			}
			name := strings.TrimSpace(fmt.Sprint(resource["name"]))
			if name != "" && name != "<nil>" {
				resourceNames = append(resourceNames, name)
			}
		}
	}
	sort.Strings(resourceNames)
	return map[string]any{
		"name":                  strings.TrimSpace(fmt.Sprint(metadata["name"])),
		"line_number":           lineNumber,
		"composite_api_version": strings.TrimSpace(fmt.Sprint(compositeRef["apiVersion"])),
		"composite_kind":        strings.TrimSpace(fmt.Sprint(compositeRef["kind"])),
		"resource_count":        len(resourceNames),
		"resource_names":        strings.Join(resourceNames, ","),
		"path":                  path,
		"lang":                  "yaml",
	}
}

func isCrossplaneClaim(apiVersion string) bool {
	if strings.HasPrefix(apiVersion, "apiextensions.crossplane.io/") {
		return false
	}
	if strings.HasPrefix(apiVersion, "pkg.crossplane.io/") {
		return false
	}
	return strings.Contains(apiVersion, ".crossplane.io/")
}

func parseCrossplaneClaim(metadata map[string]any, apiVersion string, kind string, path string, lineNumber int) map[string]any {
	return map[string]any{
		"name":        strings.TrimSpace(fmt.Sprint(metadata["name"])),
		"line_number": lineNumber,
		"kind":        kind,
		"api_version": apiVersion,
		"namespace":   strings.TrimSpace(fmt.Sprint(metadata["namespace"])),
		"path":        path,
		"lang":        "yaml",
	}
}

func parseK8sResource(document map[string]any, metadata map[string]any, apiVersion string, kind string, path string, lineNumber int) map[string]any {
	name := strings.TrimSpace(fmt.Sprint(metadata["name"]))
	namespace := strings.TrimSpace(fmt.Sprint(metadata["namespace"]))
	row := map[string]any{
		"name":           name,
		"line_number":    lineNumber,
		"kind":           kind,
		"api_version":    apiVersion,
		"namespace":      namespace,
		"qualified_name": normalizeK8sQualifiedName(namespace, kind, name),
		"path":           path,
		"lang":           "yaml",
	}
	if labels := collectMetadataLabels(metadata); labels != "" {
		row["labels"] = labels
	}
	if images := collectContainerImages(document); len(images) > 0 {
		row["container_images"] = strings.Join(images, ",")
	}
	if backends := collectHTTPRouteBackends(document); len(backends) > 0 {
		row["backend_refs"] = strings.Join(backends, ",")
	}
	return row
}

func normalizeK8sQualifiedName(namespace string, kind string, name string) string {
	parts := make([]string, 0, 3)
	if cleaned := strings.TrimSpace(namespace); cleaned != "" {
		parts = append(parts, cleaned)
	}
	if cleaned := strings.TrimSpace(kind); cleaned != "" {
		parts = append(parts, cleaned)
	}
	if cleaned := strings.TrimSpace(name); cleaned != "" {
		parts = append(parts, cleaned)
	}
	return strings.Join(parts, "/")
}

func collectContainerImages(document map[string]any) []string {
	spec, _ := document["spec"].(map[string]any)
	template := nestedMap(spec, "template")
	if len(template) == 0 {
		template = nestedMap(spec, "jobTemplate", "spec", "template")
	}
	podSpec := nestedMap(template, "spec")
	images := make([]string, 0)
	for _, key := range []string{"containers", "initContainers"} {
		if items, ok := podSpec[key].([]any); ok {
			for _, item := range items {
				container, ok := item.(map[string]any)
				if !ok {
					continue
				}
				image := strings.TrimSpace(fmt.Sprint(container["image"]))
				if image != "" && image != "<nil>" {
					images = append(images, image)
				}
			}
		}
	}
	sort.Strings(images)
	return images
}

func collectHTTPRouteBackends(document map[string]any) []string {
	spec, _ := document["spec"].(map[string]any)
	rules, _ := spec["rules"].([]any)
	backends := make([]string, 0)
	for _, item := range rules {
		rule, ok := item.(map[string]any)
		if !ok {
			continue
		}
		refs, _ := rule["backendRefs"].([]any)
		for _, ref := range refs {
			backend, ok := ref.(map[string]any)
			if !ok {
				continue
			}
			name := strings.TrimSpace(fmt.Sprint(backend["name"]))
			if name != "" && name != "<nil>" {
				backends = append(backends, name)
			}
		}
	}
	sort.Strings(backends)
	return backends
}

func nestedMap(values map[string]any, keys ...string) map[string]any {
	current := values
	for _, key := range keys {
		next, ok := current[key].(map[string]any)
		if !ok {
			return nil
		}
		current = next
	}
	return current
}

func nestedMapValue(values map[string]any, keys ...string) any {
	if len(keys) == 0 {
		return nil
	}
	current := values
	for _, key := range keys[:len(keys)-1] {
		next, ok := current[key].(map[string]any)
		if !ok {
			return nil
		}
		current = next
	}
	return current[keys[len(keys)-1]]
}
