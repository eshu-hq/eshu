package reducer

import "github.com/eshu-hq/eshu/go/internal/codeprovenance"

// resolveGenericCallee resolves a parser-emitted call/reference to a callee
// entity by an ordered fallback dispatch. It returns the callee entity id, the
// callee file path, and the closed resolution-provenance method (ADR #2222)
// describing which branch produced the match. The method records how the edge
// was resolved; it never gates resolution. An unresolved call returns empty
// strings and an empty method.
func resolveGenericCallee(
	index codeEntityIndex,
	repositoryID string,
	repositoryImports map[string][]string,
	reexportIndex codeCallReexportIndex,
	rawPath string,
	relativePath string,
	fileData map[string]any,
	call map[string]any,
) (string, string, codeprovenance.Method) {
	language := codeCallLanguage(call, rawPath, relativePath)
	if codeCallPrefersImportedQualifiedTarget(call, language) {
		if entityID, calleeFile := resolveImportedCrossFileCallee(
			index,
			repositoryImports,
			reexportIndex,
			repositoryID,
			rawPath,
			relativePath,
			fileData,
			call,
		); entityID != "" {
			return entityID, calleeFile, codeprovenance.MethodImportBinding
		}
	}

	callLine := codeCallInt(call["line_number"], call["ref_line"])
	if entityID := resolveSameFileScopedCalleeEntityID(index, rawPath, relativePath, call, callLine); entityID != "" {
		return entityID, codeCallPreferredPath(rawPath, relativePath), codeprovenance.MethodSameFile
	}
	if entityID := resolveDynamicJavaScriptCalleeEntityID(index, rawPath, relativePath, fileData, call); entityID != "" {
		return entityID, codeCallPreferredPath(rawPath, relativePath), codeprovenance.MethodTypeInferred
	}
	if entityID := resolveSameFileCalleeEntityID(index, rawPath, relativePath, call); entityID != "" {
		return entityID, codeCallPreferredPath(rawPath, relativePath), codeprovenance.MethodSameFile
	}

	if language == "go" {
		if entityID := resolveGoPackageQualifiedCalleeEntityID(index, repositoryID, fileData, call); entityID != "" {
			return entityID, index.entityFileByID[entityID], codeprovenance.MethodImportBinding
		}
		if entityID := resolveGoMethodReturnChainCalleeEntityID(index, repositoryID, call); entityID != "" {
			return entityID, index.entityFileByID[entityID], codeprovenance.MethodTypeInferred
		}
	}
	if language == "go" && !codeCallHasQualifiedScope(call, language) {
		if entityID := resolveGoSameDirectoryCalleeEntityID(index, repositoryID, rawPath, relativePath, call, language); entityID != "" {
			return entityID, index.entityFileByID[entityID], codeprovenance.MethodScopeUniqueName
		}
	}
	for _, name := range codeCallExactCandidateNames(call, language) {
		if entityID := index.uniqueNameByRepo[repositoryID][name]; entityID != "" {
			return entityID, index.entityFileByID[entityID], codeprovenance.MethodRepoUniqueName
		}
	}
	if !codeCallHasQualifiedScope(call, language) {
		for _, name := range codeCallBroadCandidateNames(call, language) {
			if entityID := index.uniqueNameByRepo[repositoryID][name]; entityID != "" {
				return entityID, index.entityFileByID[entityID], codeprovenance.MethodRepoUniqueName
			}
		}
	}

	entityID, calleeFile := resolveImportedCrossFileCallee(
		index,
		repositoryImports,
		reexportIndex,
		repositoryID,
		rawPath,
		relativePath,
		fileData,
		call,
	)
	if entityID == "" {
		return "", "", ""
	}
	return entityID, calleeFile, codeprovenance.MethodImportBinding
}
