// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package json

func composerManifestDependencyVariables(
	document map[string]any,
	lang string,
	topLevelEntries []orderedJSONEntry,
	idx *newlineIndex,
) []map[string]any {
	rows := dependencyVariablesWithScope(document, lang, "require", "composer", topLevelEntries, idx, "runtime", false)
	devRows := dependencyVariablesWithScope(document, lang, "require-dev", "composer", topLevelEntries, idx, "dev", true)
	return append(rows, devRows...)
}
