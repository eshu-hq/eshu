package reducer

import (
	"path/filepath"
	"strings"
)

// resolveGoPackageQualifiedCalleeEntityID uses parser import rows to keep
// package-qualified Go calls bounded to source directories in the same repo.
func resolveGoPackageQualifiedCalleeEntityID(
	index codeEntityIndex,
	repositoryID string,
	fileData map[string]any,
	call map[string]any,
) string {
	fullName := strings.TrimSpace(anyToString(call["full_name"]))
	name := strings.TrimSpace(anyToString(call["name"]))
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
// ctx.Actions().GetActionInstance when one same-repo Actions method return type
// proves the receiver type.
func resolveGoMethodReturnChainCalleeEntityID(
	index codeEntityIndex,
	repositoryID string,
	call map[string]any,
) string {
	fullName := strings.TrimSpace(anyToString(call["full_name"]))
	name := strings.TrimSpace(anyToString(call["name"]))
	if repositoryID == "" || fullName == "" || name == "" || !strings.Contains(fullName, "().") {
		return ""
	}
	receiverMethod := goReceiverMethodNameFromCallChain(fullName, name)
	if receiverMethod == "" {
		return ""
	}
	receiverType := index.goMethodReturnTypes[repositoryID][receiverMethod]
	if receiverType == "" {
		return ""
	}
	if entityID := index.uniqueNameByRepo[repositoryID][receiverType+"."+name]; entityID != "" {
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
	name := strings.TrimSpace(anyToString(item["name"]))
	returnType := strings.TrimSpace(anyToString(item["return_type"]))
	if repositoryID == "" || name == "" || returnType == "" {
		return
	}
	if _, ok := candidates[repositoryID]; !ok {
		candidates[repositoryID] = make(map[string]map[string]struct{})
	}
	if _, ok := candidates[repositoryID][name]; !ok {
		candidates[repositoryID][name] = make(map[string]struct{})
	}
	candidates[repositoryID][name][returnType] = struct{}{}
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

func goReceiverMethodNameFromCallChain(fullName string, terminalName string) string {
	trimmed := strings.TrimSpace(fullName)
	suffix := "." + strings.TrimSpace(terminalName)
	if suffix == "." || !strings.HasSuffix(trimmed, suffix) {
		return ""
	}
	receiverChain := strings.TrimSuffix(trimmed, suffix)
	if !strings.HasSuffix(receiverChain, "()") {
		return ""
	}
	receiverChain = strings.TrimSuffix(receiverChain, "()")
	index := strings.LastIndex(receiverChain, ".")
	if index < 0 || index == len(receiverChain)-1 {
		return receiverChain
	}
	return receiverChain[index+1:]
}

func goImportSource(entry map[string]any) string {
	if source := strings.TrimSpace(anyToString(entry["source"])); source != "" {
		return source
	}
	return strings.TrimSpace(anyToString(entry["name"]))
}

func goImportLocalName(entry map[string]any, importSource string) string {
	if alias := strings.TrimSpace(anyToString(entry["alias"])); alias != "" {
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
