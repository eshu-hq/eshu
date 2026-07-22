// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package yaml

import (
	"fmt"
	"sort"
	"strings"
)

// Kustomize-specific document detection and field extraction lives in
// kustomize_semantics.go (isKustomization, parseKustomization, and the
// collectKustomize*/collectPatch* helpers) to keep this file under the
// repo's 500-line package-file cap.

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
	if !ok {
		return ""
	}
	return collectLabelLikeMap(labels)
}

// collectLabelLikeMap normalizes any label-shaped map (metadata.labels,
// spec.selector, spec.template.metadata.labels) into a stable, sorted
// "k=v,k=v" string. Shared by collectMetadataLabels, collectSpecSelector, and
// collectPodTemplateLabels so all three normalize identically.
func collectLabelLikeMap(values map[string]any) string {
	if len(values) == 0 {
		return ""
	}
	keys := sortedMapKeysAny(values)
	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		value := strings.TrimSpace(fmt.Sprint(values[key]))
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
		"name":         cleanYAMLString(metadata["name"]),
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
		"name":                  cleanYAMLString(metadata["name"]),
		"line_number":           lineNumber,
		"composite_api_version": strings.TrimSpace(fmt.Sprint(compositeRef["apiVersion"])),
		"composite_kind":        strings.TrimSpace(fmt.Sprint(compositeRef["kind"])),
		"resource_count":        len(resourceNames),
		"resource_names":        strings.Join(resourceNames, ","),
		"path":                  path,
		"lang":                  "yaml",
	}
}

// A Crossplane Claim is intentionally NOT parser-classified here (issue
// #5347). A real Claim's apiVersion is the XRD's own custom group
// (spec.group, e.g. "database.example.org/v1alpha1") — it never contains
// ".crossplane.io/"; apiVersions that DO contain that substring belong to
// provider Managed Resources (e.g. "ec2.aws.crossplane.io/v1alpha1") or
// Crossplane's own apiextensions/pkg groups, never a Claim. A prior
// classifier keyed on the ".crossplane.io/" substring and was therefore
// inverted: it misclassified provider Managed Resources as claims while
// every real Claim fell through to k8s_resources anyway. Every non-XRD,
// non-Composition Crossplane-family document — including real Claims — now
// flows to parseK8sResource below (join keys: api_version, kind), and the
// reducer correlation layer (internal/reducer, SATISFIED_BY materialization)
// classifies a K8sResource row as a Claim by resolving
// (api_version's group, kind) against exactly one CrossplaneXRD's
// (spec.group, spec.claimNames.kind) — a graph-edge classification, not a
// parse-time label.

func parseK8sResource(document map[string]any, metadata map[string]any, apiVersion string, kind string, path string, lineNumber int) map[string]any {
	name := cleanYAMLString(metadata["name"])
	namespace := cleanYAMLString(metadata["namespace"])
	row := map[string]any{
		"name":           name,
		"line_number":    lineNumber,
		"kind":           kind,
		"api_version":    apiVersion,
		"qualified_name": normalizeK8sQualifiedName(namespace, kind, name),
		"path":           path,
		"lang":           "yaml",
	}
	if namespace != "" {
		row["namespace"] = namespace
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
	// selector is always emitted (empty string when spec.selector is absent)
	// so the query-time SELECTS matcher can distinguish "known, empty
	// selector" (genuinely selectorless Service) from "key absent"
	// (pre-upgrade data, selector truth unknown). Only Services carry a
	// meaningful selector, but the key is emitted for every kind for
	// deterministic, consistent content-row shape.
	row["selector"] = collectSpecSelector(document)
	// pod_template_labels is emitted only for pod-template-bearing kinds so
	// its absence is a meaningful "not a workload" (or "pre-upgrade data")
	// signal rather than an ambiguous empty capture. The v1 SELECTS matcher
	// only consumes this for Deployment, but all four kinds are captured so
	// widening the matcher later needs no parser change.
	if isK8sPodTemplateKind(kind) {
		row["pod_template_labels"] = collectPodTemplateLabels(document)
	}
	return row
}

// isK8sPodTemplateKind reports whether kind is a workload kind that carries
// a pod template (spec.template.metadata.labels).
func isK8sPodTemplateKind(kind string) bool {
	switch strings.TrimSpace(kind) {
	case "Deployment", "StatefulSet", "DaemonSet", "ReplicaSet":
		return true
	default:
		return false
	}
}

// collectSpecSelector normalizes spec.selector into a sorted, deterministic
// "k=v,k=v" string, mirroring collectMetadataLabels's normalization exactly
// so all label-shaped fields compare the same way downstream.
//
// spec.selector has two different shapes depending on kind:
//   - Service: a flat label map ({app: x}).
//   - Workload (Deployment/StatefulSet/DaemonSet/ReplicaSet): a nested
//     Kubernetes LabelSelector ({matchLabels: {app: x}, matchExpressions: [...]}).
//
// This is shape-aware: when spec.selector.matchLabels is present, that
// nested map is normalized; otherwise spec.selector itself is treated as
// the flat map. matchExpressions (a list of set-based operators) is not
// captured -- it cannot be flattened into the "k=v" encoding used here and
// is out of scope for this parser pass.
func collectSpecSelector(document map[string]any) string {
	spec, _ := document["spec"].(map[string]any)
	selector, _ := spec["selector"].(map[string]any)
	if matchLabels, ok := selector["matchLabels"].(map[string]any); ok {
		return collectLabelLikeMap(matchLabels)
	}
	// A LabelSelector-shaped spec.selector (workload kinds) carrying
	// matchExpressions but no matchLabels is deliberately NOT captured:
	// matchExpressions is a list of set-based operator objects, not a flat
	// label map, and a workload's own selector is never a SELECTS match key
	// (matching keys off Service.selector vs workload.pod_template_labels). Emit
	// "" rather than stringifying the raw slice-of-maps into the persisted
	// metadata. matchExpressions support is out of scope for v1.
	if _, ok := selector["matchExpressions"]; ok {
		return ""
	}
	// Flat label selector (a Service's spec.selector, e.g. {app: x}).
	return collectLabelLikeMap(selector)
}

// collectPodTemplateLabels normalizes a workload's
// spec.template.metadata.labels into the same sorted "k=v,k=v" encoding as
// collectMetadataLabels and collectSpecSelector.
func collectPodTemplateLabels(document map[string]any) string {
	spec, _ := document["spec"].(map[string]any)
	template := nestedMap(spec, "template")
	metadata := nestedMap(template, "metadata")
	labels, _ := metadata["labels"].(map[string]any)
	return collectLabelLikeMap(labels)
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
