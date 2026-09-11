// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package golang

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/shared"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// resolveGoCrossRepoExportCalleeEntityID resolves a Go package-qualified call to
// an exported top-level function defined in another repository. It returns the
// callee entity id only when the import path joins to exactly one exported
// function and that function lives in a different repository than the caller.
func resolveGoCrossRepoExportCalleeEntityID(
	index shared.EntityIndex,
	repositoryID string,
	fileData map[string]any,
	call map[string]any,
) string {
	fullName := strings.TrimSpace(payloadcore.AnyToString(call["full_name"]))
	name := strings.TrimSpace(payloadcore.AnyToString(call["name"]))
	qualifier, ok := goPackageQualifier(fullName, name)
	if !ok || !shared.GoExportedName(name) {
		return ""
	}
	for _, entry := range payloadcore.MapSlice(fileData["imports"]) {
		importSource := goImportSource(entry)
		if importSource == "" || goImportLocalName(entry, importSource) != qualifier {
			continue
		}
		importPath := shared.NormalizePath(importSource)
		candidate, found := index.GoExportByImportPath(importPath, name)
		if !found || candidate.Count != 1 || candidate.EntityID == "" {
			continue
		}
		if candidate.RepositoryID == repositoryID {
			continue
		}
		return candidate.EntityID
	}
	return ""
}
