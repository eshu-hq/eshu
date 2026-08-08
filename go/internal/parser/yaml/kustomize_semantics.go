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

// collectKustomizeResourceRefs walks `bases` as well as `resources` and
// `components`, which matters for the legacy key. `bases` and `resource_refs`
// are not complements: an entry is a base when it could be a same-repo path,
// and a resource ref when it is a remote target or a file. Walking only
// `resources`/`components` here left a remote target written under `bases:` in
// neither list — no EXTENDS_BASE edge, which is right, and no DEPLOYS_FROM
// either, which is not.
func collectKustomizeResourceRefs(document map[string]any, bases []string) []string {
	baseSet := make(map[string]struct{}, len(bases))
	for _, base := range bases {
		baseSet[base] = struct{}{}
	}

	refs := make([]string, 0)
	for _, value := range append(
		append(
			collectKustomizeStringValues(document["resources"]),
			collectKustomizeStringValues(document["components"])...,
		),
		collectKustomizeStringValues(document["bases"])...,
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
// host. "Carries a dot" is not enough of a test on its own, because a directory
// laid out by version carries one too — "v1.2/base" and "v2.0/overlays/prod"
// are ordinary same-repo paths. Reading those as remote costs a real
// EXTENDS_BASE edge and hands the string to the fuzzy catalog matcher, which
// can mint a DEPLOYS_FROM to whatever repository happens to alias "base". Both
// directions are wrong, so the dot has to be qualified rather than trusted.
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
	if !found || rest == "" || !looksLikeKustomizeHost(head) {
		return false
	}
	// A host-shaped first segment is not enough on its own. `config.prod/base`
	// is an ordinary local directory whose name happens to end in letters after
	// a dot, and treating it as remote loses its EXTENDS_BASE edge and offers
	// the string to the fuzzy catalog matcher, where it can invent a
	// DEPLOYS_FROM (#5609 review, codex). Kustomize's scheme-less remote form
	// always names a repository — host/org/repo — so require the third segment.
	return strings.Count(trimmed, "/") >= 2
}

// looksLikeKustomizeHost reports whether a first path segment is a hostname
// rather than a directory name, by looking at the label after the final dot. A
// top-level domain is letters, so "github.com" is a host; a version directory
// ends in digits — "v1.2", "1.10" — and is not.
//
// Two shapes this does not classify as a host, both deliberate: a bare IP
// address ("10.0.0.1/org/repo") and a host carrying an explicit port
// ("git.example.com:8080/org/repo"). Neither appears in Kustomize's documented
// scheme-less remote forms, and both land in bases, which is the harmless side
// — an unresolvable base produces no edge, while a wrong resource ref reaches
// the catalog matcher and can invent one.
func looksLikeKustomizeHost(segment string) bool {
	dot := strings.LastIndex(segment, ".")
	if dot < 0 {
		return false
	}
	tld := segment[dot+1:]
	if len(tld) < 2 {
		return false
	}
	for _, char := range tld {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') {
			return false
		}
	}
	return true
}
