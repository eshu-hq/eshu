package relationships

func discoverDockerComposeEvidence(
	sourceRepoID, filePath, content string,
	catalog []CatalogEntry,
	seen map[evidenceKey]struct{},
) []EvidenceFact {
	var evidence []EvidenceFact
	for _, document := range parseYAMLDocuments(content) {
		for _, candidate := range dockerComposeBuildContexts(document) {
			evidence = append(evidence, matchCatalog(
				sourceRepoID, candidate, filePath,
				EvidenceKindDockerComposeBuildContext, RelDeploysFrom, 0.91,
				"Docker Compose build context deploys from the target repository",
				"docker_compose", catalog, seen, map[string]any{
					"build_context": candidate,
				},
			)...)
		}
		for _, candidate := range dockerComposeImageRefs(document) {
			evidence = append(evidence, matchCatalog(
				sourceRepoID, candidate, filePath,
				EvidenceKindDockerComposeImage, RelDeploysFrom, 0.88,
				"Docker Compose image reference deploys from artifacts owned by the target repository",
				"docker_compose", catalog, seen, map[string]any{
					"image_ref": candidate,
				},
			)...)
		}
		for _, candidate := range dockerComposeDependsOnRefs(document) {
			evidence = append(evidence, matchCatalog(
				sourceRepoID, candidate, filePath,
				EvidenceKindDockerComposeDependsOn, RelDependsOn, 0.84,
				"Docker Compose service dependency refers to the target repository",
				"docker_compose", catalog, seen, map[string]any{
					"depends_on_service": candidate,
				},
			)...)
		}
	}
	return evidence
}

func dockerComposeBuildContexts(document map[string]any) []string {
	services, ok := nestedMap(document, "services")
	if !ok {
		return nil
	}

	contexts := make([]string, 0, len(services))
	for _, rawService := range services {
		service, ok := rawService.(map[string]any)
		if !ok {
			continue
		}
		buildValue, ok := service["build"]
		if !ok {
			continue
		}
		switch typed := buildValue.(type) {
		case string:
			if context := stringValue(typed); context != "" {
				contexts = append(contexts, context)
			}
		case map[string]any:
			if context := stringValue(typed["context"]); context != "" {
				contexts = append(contexts, context)
			}
		default:
			continue
		}
	}

	return uniqueStrings(contexts)
}

func dockerComposeImageRefs(document map[string]any) []string {
	services, ok := nestedMap(document, "services")
	if !ok {
		return nil
	}

	images := make([]string, 0, len(services))
	for _, rawService := range services {
		service, ok := rawService.(map[string]any)
		if !ok {
			continue
		}
		if image := stringValue(service["image"]); image != "" {
			images = append(images, image)
		}
	}

	return uniqueStrings(images)
}

func dockerComposeDependsOnRefs(document map[string]any) []string {
	services, ok := nestedMap(document, "services")
	if !ok {
		return nil
	}

	dependencies := make([]string, 0, len(services))
	for _, rawService := range services {
		service, ok := rawService.(map[string]any)
		if !ok {
			continue
		}
		for _, dependency := range dockerComposeDependsOnValues(service["depends_on"]) {
			if dependency != "" {
				dependencies = append(dependencies, dependency)
			}
		}
	}

	return uniqueStrings(dependencies)
}

func dockerComposeDependsOnValues(value any) []string {
	switch typed := value.(type) {
	case []any:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			if dependency := stringValue(item); dependency != "" {
				values = append(values, dependency)
			}
		}
		return values
	case map[string]any:
		values := make([]string, 0, len(typed))
		for key := range typed {
			if key != "" {
				values = append(values, key)
			}
		}
		return values
	default:
		return nil
	}
}
