package query

type contentRelationshipSpec struct {
	relationshipType string
	targetName       string
	reason           string
}

func buildOutgoingTerraformRelationships(entity EntityContent) ([]map[string]any, bool, error) {
	switch entity.EntityType {
	case "TerraformModule":
		if source, ok := metadataNonEmptyString(entity.Metadata, "source"); ok {
			if normalized := normalizeConfigArtifactExpression(source, nil); normalized != "" {
				source = normalized
			}
			return []map[string]any{
				{
					"type":        "USES_MODULE",
					"target_name": source,
					"reason":      "terraform_module_source",
				},
			}, true, nil
		}
		return nil, true, nil
	case "TerragruntConfig":
		relationships := make([]map[string]any, 0, 8)
		seen := make(map[string]struct{}, 8)
		add := func(spec contentRelationshipSpec) {
			if spec.relationshipType == "" || spec.targetName == "" || spec.reason == "" {
				return
			}
			key := spec.relationshipType + "|" + spec.targetName + "|" + spec.reason
			if _, ok := seen[key]; ok {
				return
			}
			seen[key] = struct{}{}
			relationships = append(relationships, map[string]any{
				"type":        spec.relationshipType,
				"target_name": spec.targetName,
				"reason":      spec.reason,
			})
		}
		if source, ok := metadataNonEmptyString(entity.Metadata, "terraform_source"); ok {
			if normalized := normalizeConfigArtifactExpression(source, nil); normalized != "" {
				source = normalized
			}
			add(contentRelationshipSpec{
				relationshipType: "USES_MODULE",
				targetName:       source,
				reason:           "terragrunt_terraform_source",
			})
		}
		for _, includePath := range metadataStringSlice(entity.Metadata, "include_paths") {
			includePath = normalizeConfigArtifactExpression(includePath, nil)
			if includePath == "" {
				continue
			}
			add(contentRelationshipSpec{
				relationshipType: "DISCOVERS_CONFIG_IN",
				targetName:       includePath,
				reason:           "terragrunt_include_path",
			})
		}
		for _, configPath := range metadataStringSlice(entity.Metadata, "read_config_paths") {
			configPath = normalizeConfigArtifactExpression(configPath, nil)
			if configPath == "" {
				continue
			}
			add(contentRelationshipSpec{
				relationshipType: "DISCOVERS_CONFIG_IN",
				targetName:       configPath,
				reason:           "terragrunt_read_config",
			})
		}
		for _, configPath := range metadataStringSlice(entity.Metadata, "find_in_parent_folders_paths") {
			configPath = normalizeConfigArtifactExpression(configPath, nil)
			if configPath == "" {
				continue
			}
			add(contentRelationshipSpec{
				relationshipType: "DISCOVERS_CONFIG_IN",
				targetName:       configPath,
				reason:           "terragrunt_find_in_parent_folders",
			})
		}
		for _, configPath := range metadataStringSlice(entity.Metadata, "local_config_asset_paths") {
			configPath = normalizeConfigArtifactExpression(configPath, nil)
			if configPath == "" {
				continue
			}
			add(contentRelationshipSpec{
				relationshipType: "DISCOVERS_CONFIG_IN",
				targetName:       configPath,
				reason:           "local_config_asset",
			})
		}
		return relationships, true, nil
	case "TerragruntDependency":
		if configPath, ok := metadataNonEmptyString(entity.Metadata, "config_path"); ok {
			configPath = normalizeConfigArtifactExpression(configPath, nil)
			if configPath == "" {
				return nil, true, nil
			}
			return []map[string]any{
				{
					"type":        "DISCOVERS_CONFIG_IN",
					"target_name": configPath,
					"reason":      "terragrunt_dependency_config_path",
				},
			}, true, nil
		}
		return nil, true, nil
	default:
		return nil, false, nil
	}
}
