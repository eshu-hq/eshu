// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/shared"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// FileRootCallerID returns the file path identity for top-level calls in
// JavaScript package-root files. This keeps executable module bodies visible
// to dead-code reachability without treating every library module's
// top-level expressions as roots.
func FileRootCallerID(repositoryID string, relativePath string, fileData map[string]any) string {
	language := payloadcore.AnyToString(fileData["language"])
	if language == "" {
		language = payloadcore.AnyToString(fileData["lang"])
	}
	switch strings.ToLower(language) {
	case "javascript", "jsx", "typescript", "tsx":
	default:
		return ""
	}
	for _, rootKind := range shared.DeadCodeFileRootKinds(fileData) {
		switch rootKind {
		case "javascript.node_package_entrypoint", "javascript.node_package_bin", "javascript.node_package_script", "javascript.node_package_export":
			if repositoryID == "" || relativePath == "" {
				return ""
			}
			return repositoryID + ":" + shared.NormalizePath(relativePath)
		}
	}
	return ""
}

// SameFileTopLevelCallerID promotes same-file top-level JS/TS calls to
// file-root caller edges because module-body calls execute when the file is
// loaded, even when the callee name is a project-specific factory.
func SameFileTopLevelCallerID(
	repositoryID string,
	callerFilePath string,
	calleeFilePath string,
	call map[string]any,
) string {
	if repositoryID == "" || callerFilePath == "" {
		return ""
	}
	switch strings.ToLower(shared.CallLanguage(call, callerFilePath, callerFilePath)) {
	case "javascript", "jsx", "typescript", "tsx":
	default:
		return ""
	}
	if shared.NormalizePath(callerFilePath) != shared.NormalizePath(calleeFilePath) {
		return ""
	}
	if strings.TrimSpace(payloadcore.AnyToString(call["name"])) == "" {
		return ""
	}
	return repositoryID + ":" + shared.NormalizePath(callerFilePath)
}

// TopLevelReferenceCallerID gives route-configuration references a file-root
// caller because the framework consumes the exported module object, not an
// enclosing function body.
func TopLevelReferenceCallerID(repositoryID string, callerFilePath string, call map[string]any) string {
	if repositoryID == "" || callerFilePath == "" {
		return ""
	}
	switch strings.ToLower(shared.CallLanguage(call, callerFilePath, callerFilePath)) {
	case "javascript", "jsx", "typescript", "tsx":
	default:
		return ""
	}
	switch strings.TrimSpace(payloadcore.AnyToString(call["call_kind"])) {
	case "javascript.hapi_route_handler_reference", "javascript.function_value_reference", "typescript.type_reference":
	default:
		return ""
	}
	return repositoryID + ":" + shared.NormalizePath(callerFilePath)
}
