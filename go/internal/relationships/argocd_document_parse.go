// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"net/url"
	"strings"
)

// ArgoCD document parsing: turning one parsed YAML document into the repo
// URLs, destinations and generator targets the evidence appenders in
// yaml_iac_evidence.go consume. Split out of that file so it fits under the
// repo's 500-line cap without a nolint (#5573), which also retires the stale
// audit-section citation the directive carried (#5539).

func argocdApplicationRepoURLs(document map[string]any) []string {
	spec, _ := nestedMap(document, "spec")
	if spec == nil {
		return nil
	}
	var result []string
	if source, _ := nestedMap(spec, "source"); source != nil {
		if repoURL := stringValue(source["repoURL"]); repoURL != "" {
			result = append(result, repoURL)
		}
	}
	for _, item := range sliceValue(spec["sources"]) {
		source, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if repoURL := stringValue(source["repoURL"]); repoURL != "" {
			result = append(result, repoURL)
		}
	}
	return uniqueStrings(result)
}

func argocdDocumentDestinations(document map[string]any) []argocdDestination {
	spec, _ := nestedMap(document, "spec")
	if spec == nil {
		return nil
	}

	if strings.EqualFold(stringValue(document["kind"]), "ApplicationSet") {
		template, _ := nestedMap(spec, "template")
		templateSpec, _ := nestedMap(template, "spec")
		return uniqueDestinations([]argocdDestination{argocdDestinationFromSpec(templateSpec)})
	}

	return uniqueDestinations([]argocdDestination{argocdDestinationFromSpec(spec)})
}

func argocdApplicationSetDiscoveryTargets(document map[string]any) []argocdDiscoveryTarget {
	if !strings.EqualFold(stringValue(document["kind"]), "ApplicationSet") {
		return nil
	}
	spec, _ := nestedMap(document, "spec")
	if spec == nil {
		return nil
	}
	var targets []argocdDiscoveryTarget
	for _, generator := range collectGitGenerators(sliceValue(spec["generators"])) {
		repoURL := stringValue(generator["repoURL"])
		if repoURL == "" || isArgoTemplateString(repoURL) {
			continue
		}
		for _, fileEntry := range sliceValue(generator["files"]) {
			entry, ok := fileEntry.(map[string]any)
			if !ok {
				continue
			}
			path := stringValue(entry["path"])
			if path == "" || !isArgoCDGitFileGeneratorPath(path) {
				continue
			}
			targets = append(targets, argocdDiscoveryTarget{repoURL: repoURL, path: path})
		}
	}
	return targets
}

func argocdApplicationSetTemplateSources(document map[string]any) []string {
	if !strings.EqualFold(stringValue(document["kind"]), "ApplicationSet") {
		return nil
	}
	spec, _ := nestedMap(document, "spec")
	template, _ := nestedMap(spec, "template")
	templateSpec, _ := nestedMap(template, "spec")
	if templateSpec == nil {
		return nil
	}
	var sources []string
	for _, repoURL := range argocdApplicationRepoURLs(map[string]any{"spec": templateSpec}) {
		if isArgoTemplateString(repoURL) {
			continue
		}
		sources = append(sources, repoURL)
	}
	return uniqueStrings(sources)
}

func argocdApplicationSetTemplateSourceSpecs(document map[string]any) []argocdTemplateSourceSpec {
	if !strings.EqualFold(stringValue(document["kind"]), "ApplicationSet") {
		return nil
	}
	spec, _ := nestedMap(document, "spec")
	template, _ := nestedMap(spec, "template")
	templateSpec, _ := nestedMap(template, "spec")
	if templateSpec == nil {
		return nil
	}

	var sources []argocdTemplateSourceSpec
	appendSource := func(source map[string]any) {
		if source == nil {
			return
		}
		sourceSpec := argocdTemplateSourceSpec{
			repoURL: stringValue(source["repoURL"]),
			path:    stringValue(source["path"]),
			chart:   stringValue(source["chart"]),
		}
		if sourceSpec.repoURL == "" && sourceSpec.path == "" && sourceSpec.chart == "" {
			return
		}
		sources = append(sources, sourceSpec)
	}

	if source, _ := nestedMap(templateSpec, "source"); source != nil {
		appendSource(source)
	}
	for _, item := range sliceValue(templateSpec["sources"]) {
		source, ok := item.(map[string]any)
		if !ok {
			continue
		}
		appendSource(source)
	}
	return sources
}

func argocdDestinationFromSpec(spec map[string]any) argocdDestination {
	if spec == nil {
		return argocdDestination{}
	}
	destination, _ := nestedMap(spec, "destination")
	if destination == nil {
		return argocdDestination{}
	}
	return argocdDestination{
		name:      stringValue(destination["name"]),
		namespace: stringValue(destination["namespace"]),
		server:    stringValue(destination["server"]),
	}
}

func collectGitGenerators(items []any) []map[string]any {
	var result []map[string]any
	for _, item := range items {
		node, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if gitGen, _ := nestedMap(node, "git"); gitGen != nil {
			result = append(result, gitGen)
		}
		for _, key := range []string{"matrix", "merge"} {
			if nested, _ := nestedMap(node, key); nested != nil {
				result = append(result, collectGitGenerators(sliceValue(nested["generators"]))...)
			}
		}
	}
	return result
}

func argocdDestinationPlatformID(destination argocdDestination) string {
	if isArgoTemplateString(destination.name) || isArgoTemplateString(destination.server) {
		return ""
	}
	clusterName := normalizePlatformToken(destination.name)
	if clusterName != "" {
		return "platform:kubernetes:none:cluster/" + clusterName + ":none:none"
	}
	host := normalizePlatformToken(argocdDestinationHost(destination.server))
	if host == "" {
		return ""
	}
	return "platform:kubernetes:none:server/" + host + ":none:none"
}

func argocdDestinationHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err == nil && parsed.Host != "" {
		return parsed.Host
	}
	return strings.TrimPrefix(strings.TrimPrefix(raw, "https://"), "http://")
}
