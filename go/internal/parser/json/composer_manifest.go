package json

func composerManifestDependencyVariables(
	document map[string]any,
	lang string,
	topLevelEntries []orderedJSONEntry,
) []map[string]any {
	rows := dependencyVariablesWithScope(document, lang, "require", "composer", topLevelEntries, "runtime", false)
	devRows := dependencyVariablesWithScope(document, lang, "require-dev", "composer", topLevelEntries, "dev", true)
	return append(rows, devRows...)
}
