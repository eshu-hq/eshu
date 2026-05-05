package relationships

func discoverKustomizeDocumentEvidence(
	sourceRepoID, filePath string,
	document map[string]any,
	catalog []CatalogEntry,
	seen map[evidenceKey]struct{},
) []EvidenceFact {
	var evidence []EvidenceFact

	appendValues := func(values []string, kind EvidenceKind, confidence float64, rationale string) {
		for _, value := range values {
			evidence = append(evidence, matchCatalog(
				sourceRepoID, value, filePath,
				kind, RelDeploysFrom, confidence, rationale,
				"kustomize", catalog, seen, nil,
			)...)
		}
	}

	appendValues(kustomizeResourceStrings(document), EvidenceKindKustomizeResource, 0.90,
		"Kustomize resources source deployment config from the target repository")
	appendValues(kustomizeHelmStrings(document), EvidenceKindKustomizeHelmChart, 0.89,
		"Kustomize Helm configuration deploys from the target repository")
	appendValues(kustomizeImageStrings(document), EvidenceKindKustomizeImage, 0.86,
		"Kustomize image configuration deploys artifacts from the target repository")

	return evidence
}

func kustomizeResourceStrings(document map[string]any) []string {
	return gatherStrings(document, "resources", "components")
}

func kustomizeHelmStrings(document map[string]any) []string {
	return gatherObjectStrings(document, "helmCharts", "name", "repo", "releaseName")
}

func kustomizeImageStrings(document map[string]any) []string {
	return gatherObjectStrings(document, "images", "name", "newName")
}

func gatherStrings(document map[string]any, keys ...string) []string {
	var result []string
	for _, key := range keys {
		for _, item := range sliceValue(document[key]) {
			if value := stringValue(item); value != "" {
				result = append(result, value)
			}
		}
	}
	return uniqueStrings(result)
}

func gatherObjectStrings(document map[string]any, listKey string, fieldKeys ...string) []string {
	var result []string
	for _, item := range sliceValue(document[listKey]) {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		for _, key := range fieldKeys {
			if value := stringValue(entry[key]); value != "" {
				result = append(result, value)
			}
		}
	}
	return uniqueStrings(result)
}
