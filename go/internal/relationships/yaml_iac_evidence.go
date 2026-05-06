package relationships

import (
	"io"
	"net/url"
	"strings"

	"gopkg.in/yaml.v3"
)

func discoverArgoCDDocumentEvidence(
	controlRepoID, filePath string,
	document map[string]any,
	catalog []CatalogEntry,
	seen map[evidenceKey]struct{},
	contentIndex evidenceContentIndex,
) []EvidenceFact {
	var evidence []EvidenceFact

	for _, repoURL := range argocdApplicationRepoURLs(document) {
		for _, deployedRepo := range matchingCatalogEntries(repoURL, catalog) {
			evidence = append(evidence, matchCatalog(
				controlRepoID, repoURL, filePath,
				EvidenceKindArgoCDAppSource, RelDeploysFrom, 0.95,
				"ArgoCD Application source references the target repository",
				"argocd", catalog, seen, nil,
			)...)
			for _, destination := range argocdDocumentDestinations(document) {
				evidence = append(evidence, appendDestinationPlatformEvidence(
					deployedRepo.RepoID, filePath, destination, seen,
				)...)
			}
		}
	}

	discoveryTargets := argocdApplicationSetDiscoveryTargets(document)
	templateSources := argocdApplicationSetTemplateSources(document)
	templateSourceSpecs := argocdApplicationSetTemplateSourceSpecs(document)
	if len(discoveryTargets) == 0 {
		return evidence
	}

	for _, discovery := range discoveryTargets {
		for _, configRepo := range matchingCatalogEntries(discovery.repoURL, catalog) {
			if configRepo.RepoID == controlRepoID {
				continue
			}
			evidence = append(evidence, appendDiscoveryEvidence(
				controlRepoID, configRepo, filePath, discovery.path, seen,
			)...)
			for _, templateSource := range append(
				templateSources,
				append(
					argocdEvaluatedTemplateSources(templateSourceSpecs, discovery, configRepo.RepoID, contentIndex),
					argocdConfigIdentityDeploySources(discovery, configRepo.RepoID, contentIndex)...,
				)...,
			) {
				for _, deployedRepo := range matchingCatalogEntries(templateSource, catalog) {
					if deployedRepo.RepoID == configRepo.RepoID || deployedRepo.RepoID == controlRepoID {
						continue
					}
					evidence = append(evidence, appendDeploySourceEvidence(
						controlRepoID, deployedRepo, configRepo, filePath, discovery.path, templateSource, seen,
					)...)
					for _, destination := range argocdDocumentDestinations(document) {
						evidence = append(evidence, appendDestinationPlatformEvidence(
							deployedRepo.RepoID, filePath, destination, seen,
						)...)
					}
				}
			}
		}
	}

	return evidence
}

type argocdDiscoveryTarget struct {
	repoURL string
	path    string
}

type argocdTemplateSourceSpec struct {
	repoURL string
	path    string
	chart   string
}

type argocdDestination struct {
	name      string
	namespace string
	server    string
}

func appendDiscoveryEvidence(
	controlRepoID string,
	configRepo CatalogEntry,
	filePath, discoveryPath string,
	seen map[evidenceKey]struct{},
) []EvidenceFact {
	key := evidenceKey{
		EvidenceKind: EvidenceKindArgoCDApplicationSetDiscovery,
		SourceRepoID: controlRepoID,
		TargetRepoID: configRepo.RepoID,
		Path:         filePath,
	}
	if _, ok := seen[key]; ok {
		return nil
	}
	seen[key] = struct{}{}

	return []EvidenceFact{{
		EvidenceKind:     EvidenceKindArgoCDApplicationSetDiscovery,
		RelationshipType: RelDiscoversConfigIn,
		SourceRepoID:     controlRepoID,
		TargetRepoID:     configRepo.RepoID,
		Confidence:       0.99,
		Rationale:        "ArgoCD ApplicationSet discovers config in the target repository",
		Details: map[string]any{
			"path":           filePath,
			"discovery_path": discoveryPath,
			"matched_alias":  firstAlias(configRepo),
			"extractor":      "argocd",
		},
	}}
}

func appendDeploySourceEvidence(
	controlRepoID string,
	deployedRepo, configRepo CatalogEntry,
	filePath, discoveryPath, templateSource string,
	seen map[evidenceKey]struct{},
) []EvidenceFact {
	key := evidenceKey{
		EvidenceKind: EvidenceKindArgoCDApplicationSetDeploySource,
		SourceRepoID: deployedRepo.RepoID,
		TargetRepoID: configRepo.RepoID,
		Path:         filePath,
	}
	if _, ok := seen[key]; ok {
		return nil
	}
	seen[key] = struct{}{}

	return []EvidenceFact{{
		EvidenceKind:     EvidenceKindArgoCDApplicationSetDeploySource,
		RelationshipType: RelDeploysFrom,
		SourceRepoID:     deployedRepo.RepoID,
		TargetRepoID:     configRepo.RepoID,
		Confidence:       0.99,
		Rationale:        "The deployed repository sources manifests or overlays from the config repository",
		Details: map[string]any{
			"path":                  filePath,
			"control_plane_repo_id": controlRepoID,
			"config_repo_id":        configRepo.RepoID,
			"discovery_path":        discoveryPath,
			"deploy_repo_url":       templateSource,
			"extractor":             "argocd",
			"matched_alias":         firstAlias(deployedRepo),
		},
	}}
}

func appendDestinationPlatformEvidence(
	sourceRepoID, filePath string,
	destination argocdDestination,
	seen map[evidenceKey]struct{},
) []EvidenceFact {
	platformID := argocdDestinationPlatformID(destination)
	if platformID == "" {
		return nil
	}

	key := evidenceKey{
		EvidenceKind:   EvidenceKindArgoCDDestinationPlatform,
		SourceRepoID:   sourceRepoID,
		TargetEntityID: platformID,
		Path:           filePath,
	}
	if _, ok := seen[key]; ok {
		return nil
	}
	seen[key] = struct{}{}

	return []EvidenceFact{{
		EvidenceKind:     EvidenceKindArgoCDDestinationPlatform,
		RelationshipType: RelRunsOn,
		SourceRepoID:     sourceRepoID,
		TargetEntityID:   platformID,
		Confidence:       0.97,
		Rationale:        "ArgoCD destination points at the runtime platform where the deployed repository runs",
		Details: map[string]any{
			"path":                  filePath,
			"destination_name":      destination.name,
			"destination_namespace": destination.namespace,
			"destination_server":    destination.server,
			"extractor":             "argocd",
		},
	}}
}

func parseYAMLDocuments(content string) []map[string]any {
	decoder := yaml.NewDecoder(strings.NewReader(content))
	var documents []map[string]any
	for {
		var document map[string]any
		err := decoder.Decode(&document)
		if err == nil {
			if len(document) > 0 {
				documents = append(documents, document)
			}
			continue
		}
		if err == io.EOF {
			return documents
		}
		return documents
	}
}

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

func matchingCatalogEntries(candidate string, catalog []CatalogEntry) []CatalogEntry {
	var result []CatalogEntry
	for _, entry := range catalog {
		if matchesEntry(candidate, entry) != "" {
			result = append(result, entry)
		}
	}
	return result
}

func firstAlias(entry CatalogEntry) string {
	if len(entry.Aliases) == 0 {
		return ""
	}
	return entry.Aliases[0]
}

func nestedMap(root map[string]any, key string) (map[string]any, bool) {
	if root == nil {
		return nil, false
	}
	value, ok := root[key]
	if !ok {
		return nil, false
	}
	result, ok := value.(map[string]any)
	return result, ok
}

func sliceValue(value any) []any {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	return items
}

func stringValue(value any) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func normalizePlatformToken(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return ""
	}
	replacer := strings.NewReplacer(" ", "-", "\t", "-", "\n", "-", "\r", "-")
	return replacer.Replace(raw)
}

func uniqueDestinations(values []argocdDestination) []argocdDestination {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[argocdDestination]struct{}, len(values))
	result := make([]argocdDestination, 0, len(values))
	for _, value := range values {
		if value.name == "" && value.server == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
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
	return result
}
