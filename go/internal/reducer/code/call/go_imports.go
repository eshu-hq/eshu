// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package call

import (
	"path/filepath"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// resolveGoPackageQualifiedCalleeEntityID uses parser import rows to keep
// package-qualified Go calls bounded to source directories in the same repo.
func resolveGoPackageQualifiedCalleeEntityID(
	index EntityIndex,
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
	for _, entry := range mapSlice(fileData["imports"]) {
		importSource := goImportSource(entry)
		if importSource == "" || goImportLocalName(entry, importSource) != qualifier {
			continue
		}
		for _, dir := range goImportDirectoryCandidates(importSource) {
			if entityID := index.uniqueNameByRepoDir[repositoryID][dir][name]; entityID != "" {
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
	index EntityIndex,
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
	returnType := index.goMethodReturnTypes[repositoryID][receiverType+"."+receiverMethod]
	if returnType == "" {
		return ""
	}
	if entityID := index.UniqueNameByRepo[repositoryID][returnType+"."+name]; entityID != "" {
		return entityID
	}
	return ""
}

// addGoMethodReturnTypeCandidate records return types by repo and method name
// so cross-repo packages do not make otherwise precise chains ambiguous.
func addGoMethodReturnTypeCandidate(
	candidates map[string]map[string]map[string]struct{},
	repositoryID string,
	item map[string]any,
) {
	repositoryID = strings.TrimSpace(repositoryID)
	name := strings.TrimSpace(payloadcore.AnyToString(item["name"]))
	receiverType := strings.TrimSpace(payloadcore.AnyToString(item["class_context"]))
	returnType := strings.TrimSpace(payloadcore.AnyToString(item["return_type"]))
	if repositoryID == "" || name == "" || receiverType == "" || returnType == "" {
		return
	}
	if _, ok := candidates[repositoryID]; !ok {
		candidates[repositoryID] = make(map[string]map[string]struct{})
	}
	key := receiverType + "." + name
	if _, ok := candidates[repositoryID][key]; !ok {
		candidates[repositoryID][key] = make(map[string]struct{})
	}
	candidates[repositoryID][key][returnType] = struct{}{}
}

// uniqueCodeCallNamesByDirectory keeps only directory-local names with exactly
// one entity candidate.
func uniqueCodeCallNamesByDirectory(
	dirs map[string]map[string]map[string]struct{},
) map[string]map[string]string {
	uniqueNames := make(map[string]map[string]string, len(dirs))
	for dir, names := range dirs {
		uniqueNames[dir] = make(map[string]string, len(names))
		for name, entityIDs := range names {
			if len(entityIDs) != 1 {
				continue
			}
			for entityID := range entityIDs {
				uniqueNames[dir][name] = entityID
			}
		}
	}
	return uniqueNames
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
	normalized := normalizeCodeCallPath(importSource)
	base := filepath.Base(normalized)
	if base == "." || base == string(filepath.Separator) {
		return ""
	}
	return strings.TrimSpace(base)
}

func goImportDirectoryCandidates(importSource string) []string {
	normalized := normalizeCodeCallPath(importSource)
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
