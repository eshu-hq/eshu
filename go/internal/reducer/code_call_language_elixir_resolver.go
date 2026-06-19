package reducer

import (
	"path/filepath"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

func init() {
	registerCodeCallLanguageResolvers(
		"elixir",
		codeCallLanguageResolver{
			phase:   codeCallLanguageResolverPhaseBeforeRepoFallback,
			resolve: resolveElixirAliasImportCallee,
		},
	)
}

func resolveElixirAliasImportCallee(ctx codeCallResolveContext) (string, string, codeprovenance.Method) {
	receiver, methodName, ok := elixirQualifiedCall(ctx.call)
	if !ok || ctx.repositoryID == "" {
		return "", "", ""
	}
	moduleName := elixirAliasModuleName(ctx.fileData, receiver)
	if moduleName == "" || len(ctx.repositoryImports[moduleName]) == 0 {
		return "", "", ""
	}
	entityID := ctx.index.uniqueNameByRepo[ctx.repositoryID][moduleName+"."+methodName]
	if entityID == "" {
		return "", "", ""
	}
	calleeFile := ctx.index.entityFileByID[entityID]
	if !elixirImportedModuleOwnsFile(ctx, moduleName, calleeFile) {
		return "", "", ""
	}
	return entityID, calleeFile, codeprovenance.MethodImportBinding
}

func elixirQualifiedCall(call map[string]any) (string, string, bool) {
	fullName := strings.TrimSpace(anyToString(call["full_name"]))
	dot := strings.LastIndex(fullName, ".")
	if dot <= 0 || dot >= len(fullName)-1 {
		return "", "", false
	}
	receiver := strings.TrimSpace(fullName[:dot])
	methodName := strings.TrimSpace(fullName[dot+1:])
	return receiver, methodName, receiver != "" && methodName != ""
}

func elixirAliasModuleName(fileData map[string]any, receiver string) string {
	receiver = strings.TrimSpace(receiver)
	if receiver == "" {
		return ""
	}
	for _, entry := range mapSlice(fileData["imports"]) {
		if strings.TrimSpace(anyToString(entry["import_type"])) != "alias" {
			continue
		}
		moduleName := strings.TrimSpace(anyToString(entry["name"]))
		if moduleName == "" {
			continue
		}
		alias := strings.TrimSpace(anyToString(entry["alias"]))
		if alias == "" {
			alias = codeCallTrailingName(moduleName)
		}
		if alias == receiver {
			return moduleName
		}
		if strings.HasPrefix(receiver, alias+".") {
			return moduleName + strings.TrimPrefix(receiver, alias)
		}
	}
	return ""
}

func elixirAliasCallBlocksRepoFallback(ctx codeCallResolveContext) bool {
	receiver, _, ok := elixirQualifiedCall(ctx.call)
	if !ok {
		return false
	}
	return elixirAliasModuleName(ctx.fileData, receiver) != ""
}

func elixirImportedModuleOwnsFile(ctx codeCallResolveContext, moduleName string, calleeFile string) bool {
	calleeFile = normalizeCodeCallPath(calleeFile)
	if calleeFile == "" {
		return false
	}
	candidateFiles := []string{calleeFile}
	if root := codeCallRepositoryRoot(ctx.rawPath, ctx.relativePath); root != "" && !filepath.IsAbs(calleeFile) {
		candidateFiles = append(candidateFiles, normalizeCodeCallPath(filepath.Join(root, calleeFile)))
	}
	for _, path := range ctx.repositoryImports[moduleName] {
		normalizedPath := normalizeCodeCallPath(path)
		for _, candidate := range candidateFiles {
			if normalizedPath == candidate {
				return true
			}
		}
	}
	return false
}
