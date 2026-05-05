package relationships

import "strings"

type terragruntConfigAssetSpec struct {
	field      string
	helperKind string
	reason     string
}

var terragruntConfigAssetSpecs = []terragruntConfigAssetSpec{
	{
		field:      "include_paths",
		helperKind: "include_path",
		reason:     "Terragrunt include path discovers config in the target repository",
	},
	{
		field:      "read_config_paths",
		helperKind: "read_config_path",
		reason:     "Terragrunt read_terragrunt_config path discovers config in the target repository",
	},
	{
		field:      "find_in_parent_folders_paths",
		helperKind: "find_in_parent_folders_path",
		reason:     "Terragrunt find_in_parent_folders path discovers config in the target repository",
	},
	{
		field:      "local_config_asset_paths",
		helperKind: "local_config_asset_path",
		reason:     "Terragrunt local file or templatefile path discovers config in the target repository",
	},
}

func discoverStructuredTerragruntConfigEvidence(
	sourceRepoID, filePath string,
	parsedFileData map[string]any,
	catalog []CatalogEntry,
	seen map[evidenceKey]struct{},
) []EvidenceFact {
	configs, ok := parsedFileData["terragrunt_configs"].([]any)
	if !ok {
		return nil
	}

	var evidence []EvidenceFact
	for _, item := range configs {
		config, ok := item.(map[string]any)
		if !ok {
			continue
		}
		for _, spec := range terragruntConfigAssetSpecs {
			for _, candidate := range payloadCSV(config, spec.field) {
				evidence = append(evidence, matchCatalog(
					sourceRepoID,
					candidate,
					filePath,
					EvidenceKindTerragruntConfigAssetPath,
					RelDiscoversConfigIn,
					0.88,
					spec.reason,
					"terragrunt-helper-config",
					catalog,
					seen,
					map[string]any{
						"config_path": candidate,
						"helper_kind": spec.helperKind,
					},
				)...)
			}
		}
	}

	return evidence
}

func payloadCSV(payload map[string]any, key string) []string {
	value := strings.TrimSpace(payloadString(payload, key))
	if value == "" {
		return nil
	}

	parts := strings.Split(value, ",")
	values := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		candidate := strings.TrimSpace(part)
		if candidate == "" {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		values = append(values, candidate)
	}
	return values
}
