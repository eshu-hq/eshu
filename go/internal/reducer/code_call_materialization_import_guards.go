package reducer

import "strings"

func codeCallHasExplicitImportedTarget(
	fileData map[string]any,
	call map[string]any,
) bool {
	return len(codeCallImportedTargets(mapSlice(fileData["imports"]), call)) > 0
}

func resolvePythonImportedRepositorySymbolTarget(
	index codeEntityIndex,
	language string,
	rawPath string,
	relativePath string,
	importSource string,
	paths []string,
	symbolName string,
) (string, string) {
	if language != "python" || len(paths) != 1 {
		return "", ""
	}
	path := normalizeCodeCallPath(paths[0])
	if path == "" {
		return "", ""
	}
	if !codeCallPythonImportSourceCanContainPath(rawPath, relativePath, importSource, path) {
		return "", ""
	}
	entityID := index.uniqueNameByPath[path][symbolName]
	if entityID == "" {
		return "", ""
	}
	return entityID, index.entityFileByID[entityID]
}

func codeCallPythonImportSourceCanContainPath(
	rawPath string,
	relativePath string,
	importSource string,
	targetPath string,
) bool {
	repositoryRoot := codeCallRepositoryRoot(rawPath, relativePath)
	if repositoryRoot == "" {
		return false
	}
	targetPath = normalizeCodeCallPath(targetPath)
	if targetPath == "" {
		return false
	}
	relativeTarget := strings.TrimPrefix(targetPath, normalizeCodeCallPath(repositoryRoot)+"/")
	if relativeTarget == targetPath {
		return false
	}
	modulePath := strings.ReplaceAll(strings.Trim(strings.TrimSpace(importSource), "."), ".", "/")
	if modulePath == "" || strings.Contains(modulePath, "/../") {
		return false
	}
	return relativeTarget == modulePath+".py" ||
		relativeTarget == modulePath+"/__init__.py" ||
		strings.HasPrefix(relativeTarget, modulePath+"/")
}

func resolvePythonImportedSourceCandidateTarget(
	index codeEntityIndex,
	language string,
	rawPath string,
	relativePath string,
	target codeCallImportedTarget,
) (string, string) {
	if language != "python" {
		return "", ""
	}
	matches := make(map[string]struct{})
	for _, path := range codeCallImportSourceCandidates(
		rawPath,
		relativePath,
		target.importSource,
		language,
	) {
		entityID := index.uniqueNameByPath[path][target.symbolName]
		if entityID != "" {
			matches[entityID] = struct{}{}
		}
	}
	if len(matches) != 1 {
		return "", ""
	}
	for entityID := range matches {
		return entityID, index.entityFileByID[entityID]
	}
	return "", ""
}
