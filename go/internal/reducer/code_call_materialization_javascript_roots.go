package reducer

import "strings"

// resolveFileRootCodeCallCallerID returns the file path identity for top-level
// calls in JavaScript package-root files. This keeps executable module bodies
// visible to dead-code reachability without treating every library module's
// top-level expressions as roots.
func resolveFileRootCodeCallCallerID(repositoryID string, relativePath string, fileData map[string]any) string {
	language := anyToString(fileData["language"])
	if language == "" {
		language = anyToString(fileData["lang"])
	}
	switch strings.ToLower(language) {
	case "javascript", "jsx", "typescript", "tsx":
	default:
		return ""
	}
	for _, rootKind := range toStringSlice(fileData["dead_code_file_root_kinds"]) {
		switch rootKind {
		case "javascript.node_package_entrypoint", "javascript.node_package_bin", "javascript.node_package_script", "javascript.node_package_export":
			if repositoryID == "" || relativePath == "" {
				return ""
			}
			return repositoryID + ":" + normalizeCodeCallPath(relativePath)
		}
	}
	return ""
}

// resolveSameFileTopLevelCodeCallCallerID promotes same-file top-level JS/TS
// calls to file-root caller edges because module-body calls execute when the
// file is loaded, even when the callee name is a project-specific factory.
func resolveSameFileTopLevelCodeCallCallerID(
	repositoryID string,
	callerFilePath string,
	calleeFilePath string,
	call map[string]any,
) string {
	if repositoryID == "" || callerFilePath == "" {
		return ""
	}
	switch strings.ToLower(codeCallLanguage(call, callerFilePath, callerFilePath)) {
	case "javascript", "jsx", "typescript", "tsx":
	default:
		return ""
	}
	if normalizeCodeCallPath(callerFilePath) != normalizeCodeCallPath(calleeFilePath) {
		return ""
	}
	if strings.TrimSpace(anyToString(call["name"])) == "" {
		return ""
	}
	return repositoryID + ":" + normalizeCodeCallPath(callerFilePath)
}

// resolveJavaScriptTopLevelReferenceCallerID gives route-configuration
// references a file-root caller because the framework consumes the exported
// module object, not an enclosing function body.
func resolveJavaScriptTopLevelReferenceCallerID(repositoryID string, callerFilePath string, call map[string]any) string {
	if repositoryID == "" || callerFilePath == "" {
		return ""
	}
	switch strings.ToLower(codeCallLanguage(call, callerFilePath, callerFilePath)) {
	case "javascript", "jsx", "typescript", "tsx":
	default:
		return ""
	}
	switch strings.TrimSpace(anyToString(call["call_kind"])) {
	case "javascript.hapi_route_handler_reference", "javascript.function_value_reference", "typescript.type_reference":
	default:
		return ""
	}
	return repositoryID + ":" + normalizeCodeCallPath(callerFilePath)
}
