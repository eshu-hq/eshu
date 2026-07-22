// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	codegraphv1 "github.com/eshu-hq/eshu/sdk/go/factschema/codegraph/v1"

	"github.com/eshu-hq/eshu/sdk/go/factschema"
)

type terragruntConfigAssetSpec struct {
	values     func(codegraphv1.TerragruntConfig) string
	helperKind string
	reason     string
}

var terragruntConfigAssetSpecs = []terragruntConfigAssetSpec{
	{
		values:     func(c codegraphv1.TerragruntConfig) string { return c.IncludePaths },
		helperKind: "include_path",
		reason:     "Terragrunt include path discovers config in the target repository",
	},
	{
		values:     func(c codegraphv1.TerragruntConfig) string { return c.ReadConfigPaths },
		helperKind: "read_config_path",
		reason:     "Terragrunt read_terragrunt_config path discovers config in the target repository",
	},
	{
		values:     func(c codegraphv1.TerragruntConfig) string { return c.FindInParentFoldersPaths },
		helperKind: "find_in_parent_folders_path",
		reason:     "Terragrunt find_in_parent_folders path discovers config in the target repository",
	},
	{
		values:     func(c codegraphv1.TerragruntConfig) string { return c.LocalConfigAssetPaths },
		helperKind: "local_config_asset_path",
		reason:     "Terragrunt local file or templatefile path discovers config in the target repository",
	},
}

// discoverStructuredTerragruntConfigEvidence reads the parsed_file_data
// terragrunt_configs inner key through the typed
// factschema.DecodeParsedFileDataTerragruntConfigs accessor (issue #5445
// slice 1) rather than a raw map lookup. The accessor skips a malformed row
// rather than failing the whole bucket, so a decode error here is always
// nil in practice; the error return is ignored deliberately, matching the
// pre-typing raw-map read's silent tolerance of an absent/wrong-shape
// bucket. Each helper-path field stays a comma-joined string on the typed
// struct (csvValues does the same split it always did).
func discoverStructuredTerragruntConfigEvidence(
	sourceRepoID, filePath string,
	parsedFileData map[string]any,
	matcher *catalogMatcher,
	seen map[evidenceKey]struct{},
) []EvidenceFact {
	configs, _ := factschema.DecodeParsedFileDataTerragruntConfigs(parsedFileData)

	var evidence []EvidenceFact
	for _, config := range configs {
		for _, spec := range terragruntConfigAssetSpecs {
			for _, candidate := range csvValues(spec.values(config)) {
				evidence = append(evidence, matchCatalog(
					sourceRepoID,
					candidate,
					filePath,
					EvidenceKindTerragruntConfigAssetPath,
					RelDiscoversConfigIn,
					DefaultConfidenceRegistry.ConfidenceFor(EvidenceKindTerragruntConfigAssetPath),
					spec.reason,
					"terragrunt-helper-config",
					matcher,
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
