// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package golang

import (
	"path/filepath"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/shared"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// resolveGoPackageQualifiedCalleeEntityID uses parser import rows to keep
// package-qualified Go calls bounded to source directories in the same repo.
func resolveGoPackageQualifiedCalleeEntityID(
	index shared.EntityIndex,
	repositoryID string,
	fileData map[string]any,
	call map[string]any,
) string {
	fullName := strings.TrimSpace(payloadcore.AnyToString(call["full_name"]))
	name := strings.TrimSpace(payloadcore.AnyToString(call["name"]))
	qualifier, ok := goPackageQualifier(fullName, name)
	if repositoryID == "" || !ok {
		return ""
	}
	for _, entry := range payloadcore.MapSlice(fileData["imports"]) {
		importSource := goImportSource(entry)
		if importSource == "" || goImportLocalName(entry, importSource) != qualifier {
			continue
		}
		for _, dir := range goImportDirectoryCandidates(importSource) {
			if entityID := index.UniqueNameByRepoDir(repositoryID, dir, name); entityID != "" {
				return entityID
			}
		}
	}
	return ""
}

// resolveGoMethodReturnChainCalleeEntityID links chains such as
// ctx.Actions().GetActionInstance when parser metadata proves the chain receiver
// type and one same-repo method return type exists for that receiver.
func resolveGoMethodReturnChainCalleeEntityID(
	index shared.EntityIndex,
	repositoryID string,
	call map[string]any,
) string {
	fullName := strings.TrimSpace(payloadcore.AnyToString(call["full_name"]))
	name := strings.TrimSpace(payloadcore.AnyToString(call["name"]))
	if repositoryID == "" || fullName == "" || name == "" || !strings.Contains(fullName, "().") {
		return ""
	}
	receiverType := strings.TrimSpace(payloadcore.AnyToString(call["chain_receiver_obj_type"]))
	receiverMethod := strings.TrimSpace(payloadcore.AnyToString(call["chain_receiver_method"]))
	if receiverType == "" || receiverMethod == "" {
		return ""
	}
	returnType := index.GoMethodReturnTypes(repositoryID, receiverType+"."+receiverMethod)
	if returnType == "" {
		return ""
	}
	if entityID := index.UniqueNameByRepo[repositoryID][returnType+"."+name]; entityID != "" {
		return entityID
	}
	return ""
}

// resolveGoSameDirectoryCalleeEntityID resolves an unqualified Go call to the
// unique declaration of that name within the caller's own directory.
func resolveGoSameDirectoryCalleeEntityID(
	index shared.EntityIndex,
	repositoryID string,
	rawPath string,
	relativePath string,
	call map[string]any,
	language string,
) string {
	dir := shared.DirectoryKey(shared.PreferredPath(rawPath, relativePath))
	if repositoryID == "" || dir == "" {
		return ""
	}
	for _, name := range shared.ExactCandidateNames(call, language) {
		if entityID := index.UniqueNameByRepoDir(repositoryID, dir, name); entityID != "" {
			return entityID
		}
	}
	for _, name := range shared.BroadCandidateNames(call, language) {
		if entityID := index.UniqueNameByRepoDir(repositoryID, dir, name); entityID != "" {
			return entityID
		}
	}
	return ""
}

func goPackageQualifier(fullName string, terminalName string) (string, bool) {
	fullName = strings.TrimSpace(fullName)
	terminalName = strings.TrimSpace(terminalName)
	if fullName == "" || terminalName == "" || strings.Contains(fullName, "()") {
		return "", false
	}
	suffix := "." + terminalName
	if !strings.HasSuffix(fullName, suffix) {
		return "", false
	}
	qualifier := strings.TrimSuffix(fullName, suffix)
	if qualifier == "" || strings.Contains(qualifier, ".") {
		return "", false
	}
	return qualifier, true
}

func goImportSource(entry map[string]any) string {
	if source := strings.TrimSpace(payloadcore.AnyToString(entry["source"])); source != "" {
		return source
	}
	return strings.TrimSpace(payloadcore.AnyToString(entry["name"]))
}

func goImportLocalName(entry map[string]any, importSource string) string {
	if alias := strings.TrimSpace(payloadcore.AnyToString(entry["alias"])); alias != "" {
		return alias
	}
	normalized := shared.NormalizePath(importSource)
	base := filepath.Base(normalized)
	if base == "." || base == string(filepath.Separator) {
		return ""
	}
	return strings.TrimSpace(base)
}

func goImportDirectoryCandidates(importSource string) []string {
	normalized := shared.NormalizePath(importSource)
	if normalized == "" {
		return nil
	}
	parts := strings.Split(normalized, "/")
	candidates := make([]string, 0, len(parts))
	for index := 0; index < len(parts); index++ {
		candidate := strings.Join(parts[index:], "/")
		if candidate == "" || candidate == "." {
			continue
		}
		candidates = append(candidates, candidate)
	}
	return candidates
}
