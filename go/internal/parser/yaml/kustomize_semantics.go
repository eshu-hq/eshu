// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package yaml

import (
	"fmt"
	"sort"
	"strings"
)

// isKustomization reports whether a document is a generic Kustomize build
// manifest: an explicit "kustomize.config.k8s.io/*" apiVersion (any
// version), or a kustomization.yaml/.yml file carrying no apiVersion at all.
//
// It intentionally does NOT match on a bare "kustomize" apiVersion prefix.
// That used to also capture Flux's "kustomize.toolkit.fluxcd.io/*"
// Kustomization CRs (issue #5342), misrouting them into this generic
// top-level-key parser even though a Flux Kustomization nests its
// declarative fields under spec (sourceRef, path, targetNamespace) instead
// of carrying them at the document root. See isFluxKustomization for the
// dedicated Flux path.
//
// An explicit, non-generic apiVersion vetoes the filename-only branch: a
// foreign CRD (Flux's Kustomization group, or any other apiVersion) saved as
// kustomization.yaml must not re-enter this generic path just because of its
// file name.
func isKustomization(apiVersion string, filename string) bool {
	if strings.HasPrefix(apiVersion, "kustomize.config.k8s.io/") {
		return true
	}
	// A version-less "kustomize.config.k8s.io" (no "/version" suffix) falls
	// through to the veto below and is routed to the generic k8s_resources
	// fallthrough rather than kustomize_overlays. This is accepted: a real
	// Kubernetes apiVersion always carries a version segment, so this shape
	// never occurs from a genuine manifest.
	if strings.TrimSpace(apiVersion) != "" {
		return false
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

// isRemoteKustomizeRef reports whether a Kustomize `resources`/`bases` entry
// resolves to a different repository rather than a path inside this one. It
// decides which of the two lists an entry lands in: a remote entry belongs in
// resource_refs, where the catalog matcher can raise a cross-repo DEPLOYS_FROM
// edge, and a local one belongs in bases.
//
// A scheme is not the test. Kustomize's remote-target documentation lists
// scheme-less forms as supported — "github.com/Liujingfang1/mysql",
// "github.com/Liujingfang1/mysql?ref=test" — alongside the SSH shorthand
// "git@github.com:owner/repo//someSubdir" and go-getter's "git::" forced
// protocol. Testing only for "://" filed every one of those under bases, so the
// graph claimed another repository was a directory in this one and the typed
// DEPLOYS_FROM path never saw it (#5609).
//
// The general case is the last rule: a first path segment that looks like a
// host — it carries a dot and something follows it. That is also where the
// judgement call sits. A local directory named "v1.2/base" would be read as
// remote. Erring that way is deliberate: a misread entry lands in resource_refs
// and is offered to the catalog matcher, which is exactly where the raw-content
// evidence path puts every resources entry today, so the wrong answer costs a
// catalog lookup that finds nothing rather than a false claim in the graph.
func isRemoteKustomizeRef(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	if strings.Contains(trimmed, "://") || strings.HasPrefix(trimmed, "git::") ||
		strings.HasPrefix(trimmed, "git@") {
		return true
	}
	// An explicitly relative or absolute path is a path, whatever it contains.
	if strings.HasPrefix(trimmed, "./") || strings.HasPrefix(trimmed, "../") ||
		strings.HasPrefix(trimmed, "/") {
		return false
	}
	head, rest, found := strings.Cut(trimmed, "/")
	return found && rest != "" && strings.Contains(head, ".")
}
