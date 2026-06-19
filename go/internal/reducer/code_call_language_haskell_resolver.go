package reducer

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

func init() {
	registerCodeCallLanguageResolvers(
		"haskell",
		codeCallLanguageResolver{
			phase:   codeCallLanguageResolverPhaseBeforeRepoFallback,
			resolve: resolveHaskellQualifiedImportCallee,
		},
	)
}

func resolveHaskellQualifiedImportCallee(ctx codeCallResolveContext) (string, string, codeprovenance.Method) {
	moduleName, methodName, ok := haskellQualifiedImportTarget(ctx)
	if !ok || ctx.repositoryID == "" {
		return "", "", ""
	}
	paths := ctx.repositoryImports[moduleName]
	if len(paths) == 0 {
		return "", "", ""
	}

	var resolvedEntityID string
	for _, path := range paths {
		path = normalizeCodeCallPath(path)
		if path == "" {
			continue
		}
		entityID := ctx.index.uniqueNameByPath[path][methodName]
		if entityID == "" || entityID == resolvedEntityID {
			continue
		}
		if resolvedEntityID != "" {
			return "", "", ""
		}
		resolvedEntityID = entityID
	}
	if resolvedEntityID == "" {
		return "", "", ""
	}
	return resolvedEntityID, ctx.index.entityFileByID[resolvedEntityID], codeprovenance.MethodImportBinding
}

func haskellQualifiedImportTarget(ctx codeCallResolveContext) (string, string, bool) {
	receiver, methodName, ok := haskellQualifiedCall(ctx.call)
	if !ok {
		return "", "", false
	}
	moduleName, ok := haskellImportedModuleName(ctx.fileData, receiver)
	if !ok {
		return "", "", false
	}
	return moduleName, methodName, true
}

func haskellQualifiedCall(call map[string]any) (string, string, bool) {
	fullName := strings.TrimSpace(anyToString(call["full_name"]))
	dot := strings.LastIndex(fullName, ".")
	if dot <= 0 || dot >= len(fullName)-1 {
		return "", "", false
	}
	receiver := strings.TrimSpace(fullName[:dot])
	methodName := strings.TrimSpace(fullName[dot+1:])
	return receiver, methodName, receiver != "" && methodName != ""
}

func haskellImportedModuleName(fileData map[string]any, receiver string) (string, bool) {
	receiver = strings.TrimSpace(receiver)
	if receiver == "" {
		return "", false
	}
	for _, entry := range mapSlice(fileData["imports"]) {
		if lang := strings.TrimSpace(anyToString(entry["lang"])); lang != "" && lang != "haskell" {
			continue
		}
		moduleName := strings.TrimSpace(anyToString(entry["name"]))
		if moduleName == "" {
			continue
		}
		alias := strings.TrimSpace(anyToString(entry["alias"]))
		if alias == "" {
			alias = moduleName
		}
		if receiver == alias || receiver == moduleName {
			return moduleName, true
		}
	}
	return "", false
}
