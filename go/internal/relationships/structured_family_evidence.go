package relationships

import "strings"

func discoverStructuredHelmEvidence(
	sourceRepoID, filePath string,
	parsedFileData map[string]any,
	catalog []CatalogEntry,
	seen map[evidenceKey]struct{},
) []EvidenceFact {
	var evidence []EvidenceFact

	if charts, ok := parsedFileData["helm_charts"].([]any); ok {
		for _, item := range charts {
			chart, ok := item.(map[string]any)
			if !ok {
				continue
			}
			chartName := strings.TrimSpace(payloadString(chart, "name"))
			for _, candidate := range csvValues(chart["dependencies"]) {
				details := withFirstPartyRefDetails(
					map[string]any{"helm_chart_name": chartName},
					"helm_dependency_name", chartName, "", "", "", candidate,
				)
				evidence = append(evidence, matchCatalog(
					sourceRepoID, candidate, filePath,
					EvidenceKindHelmChart, RelDeploysFrom, 0.90,
					"Helm chart metadata references the target repository",
					"helm", catalog, seen, details,
				)...)
			}
			for _, candidate := range csvValues(chart["dependency_repositories"]) {
				normalized := normalizeHelmRefValue(candidate)
				details := withFirstPartyRefDetails(
					map[string]any{
						"helm_chart_name":       chartName,
						"dependency_repository": candidate,
					},
					"helm_dependency_repository", chartName, "", "", "", normalized,
				)
				evidence = append(evidence, matchCatalog(
					sourceRepoID, normalized, filePath,
					EvidenceKindHelmChart, RelDeploysFrom, 0.90,
					"Helm chart metadata references the target repository",
					"helm", catalog, seen, details,
				)...)
			}
		}
	}

	if valuesRows, ok := parsedFileData["helm_values"].([]any); ok {
		for _, item := range valuesRows {
			valuesRow, ok := item.(map[string]any)
			if !ok {
				continue
			}
			valuesName := strings.TrimSpace(payloadString(valuesRow, "name"))
			for _, candidate := range csvValues(valuesRow["image_repositories"]) {
				normalized := normalizeHelmRefValue(candidate)
				details := withFirstPartyRefDetails(
					map[string]any{
						"helm_values_name": valuesName,
						"image_repository": candidate,
					},
					"helm_image_repository", valuesName, "", "", "", normalized,
				)
				evidence = append(evidence, matchCatalog(
					sourceRepoID, normalized, filePath,
					EvidenceKindHelmValues, RelDeploysFrom, 0.84,
					"Helm values reference the target repository",
					"helm", catalog, seen, details,
				)...)
			}
		}
	}

	return evidence
}

func discoverStructuredArgoCDEvidence(
	sourceRepoID, filePath string,
	parsedFileData map[string]any,
	catalog []CatalogEntry,
	seen map[evidenceKey]struct{},
) []EvidenceFact {
	var evidence []EvidenceFact

	if applications, ok := parsedFileData["argocd_applications"].([]any); ok {
		for _, item := range applications {
			application, ok := item.(map[string]any)
			if !ok {
				continue
			}
			appName := strings.TrimSpace(payloadString(application, "name"))
			for _, source := range argoApplicationSourceRefs(application) {
				details := withFirstPartyRefDetails(
					map[string]any{
						"argocd_application_name": appName,
						"source_revision":         source.revision,
					},
					"argocd_application_source",
					appName,
					source.path,
					source.root,
					source.revision,
					source.repoURL,
				)
				evidence = append(evidence, matchCatalog(
					sourceRepoID, source.repoURL, filePath,
					EvidenceKindArgoCDAppSource, RelDeploysFrom, 0.95,
					"ArgoCD Application source references the target repository",
					"argocd", catalog, seen, details,
				)...)

				for _, deployedRepo := range matchingCatalogEntries(source.repoURL, catalog) {
					evidence = append(evidence, appendDestinationPlatformEvidence(
						deployedRepo.RepoID,
						filePath,
						argocdDestination{
							name:      payloadString(application, "dest_name"),
							namespace: payloadString(application, "dest_namespace"),
							server:    payloadString(application, "dest_server"),
						},
						seen,
					)...)
				}
			}
		}
	}

	if appSets, ok := parsedFileData["argocd_applicationsets"].([]any); ok {
		for _, item := range appSets {
			appSet, ok := item.(map[string]any)
			if !ok {
				continue
			}
			appSetName := strings.TrimSpace(payloadString(appSet, "name"))
			discoveryRepos := csvValues(appSet["generator_source_repos"])
			discoveryPaths := csvValues(appSet["generator_source_paths"])
			discoveryRoots := csvValues(appSet["generator_source_roots"])
			if len(discoveryRoots) == 0 {
				discoveryRoots = csvValues(appSet["source_roots"])
			}
			templateRepos := csvValues(appSet["template_source_repos"])
			templatePaths := csvValues(appSet["template_source_paths"])
			templateRoots := csvValues(appSet["template_source_roots"])
			if len(templateRoots) == 0 {
				templateRoots = csvValues(appSet["source_roots"])
			}

			for _, repoURL := range discoveryRepos {
				root := firstCSV(discoveryRoots)
				path := firstCSV(discoveryPaths)
				for _, configRepo := range matchingCatalogEntries(repoURL, catalog) {
					if configRepo.RepoID == sourceRepoID {
						continue
					}
					evidence = append(evidence, appendDiscoveryEvidence(
						sourceRepoID, configRepo, filePath, path, seen,
					)...)
					applyStructuredRefDetails(evidence, EvidenceKindArgoCDApplicationSetDiscovery, configRepo.RepoID, func(details map[string]any) map[string]any {
						return withFirstPartyRefDetails(
							mergeDetails(details, map[string]any{"argocd_applicationset_name": appSetName}),
							"argocd_applicationset_discovery", appSetName, path, root, "", repoURL,
						)
					})

					for _, templateRepoURL := range templateRepos {
						templatePath := firstCSV(templatePaths)
						templateRoot := firstCSV(templateRoots)
						for _, deployedRepo := range matchingCatalogEntries(templateRepoURL, catalog) {
							if deployedRepo.RepoID == configRepo.RepoID || deployedRepo.RepoID == sourceRepoID {
								continue
							}
							evidence = append(evidence, appendDeploySourceEvidence(
								sourceRepoID, deployedRepo, configRepo, filePath, path, templateRepoURL, seen,
							)...)
							applyStructuredRefDetails(evidence, EvidenceKindArgoCDApplicationSetDeploySource, configRepo.RepoID, func(details map[string]any) map[string]any {
								return withFirstPartyRefDetails(
									mergeDetails(details, map[string]any{"argocd_applicationset_name": appSetName}),
									"argocd_applicationset_template_source", appSetName, templatePath, templateRoot, "", templateRepoURL,
								)
							})
							evidence = append(evidence, appendDestinationPlatformEvidence(
								deployedRepo.RepoID,
								filePath,
								argocdDestination{
									name:      payloadString(appSet, "dest_name"),
									namespace: payloadString(appSet, "dest_namespace"),
									server:    payloadString(appSet, "dest_server"),
								},
								seen,
							)...)
						}
					}
				}
			}
		}
	}

	return evidence
}

type argoApplicationSourceRef struct {
	repoURL  string
	path     string
	root     string
	revision string
}

func argoApplicationSourceRefs(application map[string]any) []argoApplicationSourceRef {
	repos := tupleCSVValues(application["source_repos"])
	if len(repos) == 0 {
		repos = csvValues(application["source_repo"])
	}
	paths := tupleCSVValues(application["source_paths"])
	roots := tupleCSVValues(application["source_roots"])
	revisions := tupleCSVValues(application["source_revisions"])
	if len(repos) == 1 {
		paths = fallbackCSV(paths, application["source_path"])
		roots = fallbackCSV(roots, application["source_root"])
		revisions = fallbackCSV(revisions, application["source_revision"])
	}

	refs := make([]argoApplicationSourceRef, 0, len(repos))
	for index, repoURL := range repos {
		repoURL = strings.TrimSpace(repoURL)
		if repoURL == "" {
			continue
		}
		refs = append(refs, argoApplicationSourceRef{
			repoURL:  repoURL,
			path:     indexedCSV(paths, index),
			root:     indexedCSV(roots, index),
			revision: indexedCSV(revisions, index),
		})
	}
	return refs
}

func tupleCSVValues(value any) []string {
	var values []string
	switch typed := value.(type) {
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}
		parts := strings.Split(typed, ",")
		values = make([]string, len(parts))
		for index, part := range parts {
			values[index] = strings.TrimSpace(part)
		}
	case []string:
		values = make([]string, len(typed))
		for index, part := range typed {
			values[index] = strings.TrimSpace(part)
		}
	case []any:
		values = make([]string, len(typed))
		for index, part := range typed {
			if value, ok := part.(string); ok {
				values[index] = strings.TrimSpace(value)
			}
		}
	default:
		return nil
	}
	if !hasNonEmptyCSVValue(values) {
		return nil
	}
	return values
}

func hasNonEmptyCSVValue(values []string) bool {
	for _, value := range values {
		if value != "" {
			return true
		}
	}
	return false
}

func fallbackCSV(values []string, fallback any) []string {
	if len(values) > 0 {
		return values
	}
	return csvValues(fallback)
}

func indexedCSV(values []string, index int) string {
	if index < 0 || index >= len(values) {
		return ""
	}
	return values[index]
}

func applyStructuredRefDetails(
	evidence []EvidenceFact,
	kind EvidenceKind,
	targetRepoID string,
	update func(map[string]any) map[string]any,
) {
	for index := range evidence {
		if evidence[index].EvidenceKind != kind {
			continue
		}
		if targetRepoID != "" && evidence[index].TargetRepoID != targetRepoID {
			continue
		}
		evidence[index].Details = update(evidence[index].Details)
	}
}

func firstCSV(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
