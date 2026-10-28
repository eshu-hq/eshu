// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package json

func composerManifestDependencyVariables(
	document map[string]any,
	lang string,
	topLevelEntries []orderedJSONEntry,
	idx *newlineIndex,
	evidence ComposerRuntimeEvidence,
) []map[string]any {
	rows := dependencyVariablesWithScope(document, lang, "require", "composer", topLevelEntries, idx, "runtime", false)
	// The PHP row shares the typed interpretation with runtime emission.
	// A rejected or unpositioned value must not enter parsed_file_data.
	var php *ComposerRuntimeValue
	for i := range evidence.Values {
		if evidence.Values[i].SourceKind == "composer_json_require_php" {
			php = &evidence.Values[i]
			break
		}
	}
	kept := rows[:0]
	for _, row := range rows {
		if row["name"] != "php" {
			kept = append(kept, row)
			continue
		}
		if php == nil {
			continue
		}
		row["value"] = php.Value
		row["line_number"] = php.Line
		kept = append(kept, row)
	}
	rows = kept
	devRows := dependencyVariablesWithScope(document, lang, "require-dev", "composer", topLevelEntries, idx, "dev", true)
	return append(rows, devRows...)
}
