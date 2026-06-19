package reducer

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

func init() {
	registerCodeCallLanguageResolvers(
		"perl",
		codeCallLanguageResolver{
			phase:   codeCallLanguageResolverPhaseBeforeRepoFallback,
			resolve: resolvePerlPackageImportCallee,
		},
	)
}

func resolvePerlPackageImportCallee(ctx codeCallResolveContext) (string, string, codeprovenance.Method) {
	packageName, methodName, ok := perlPackageQualifiedCall(ctx.call)
	if !ok || ctx.repositoryID == "" {
		return "", "", ""
	}
	if !perlFileImportsPackage(ctx.fileData, packageName) {
		return "", "", ""
	}
	paths := ctx.repositoryImports[packageName]
	if len(paths) == 0 {
		return "", "", ""
	}

	var resolvedEntityID string
	for _, path := range paths {
		path = normalizeCodeCallPath(path)
		if path == "" {
			continue
		}
		for _, candidateName := range perlImportedFunctionCandidateNames(packageName, methodName) {
			entityID := ctx.index.uniqueNameByPath[path][candidateName]
			if entityID == "" || entityID == resolvedEntityID {
				continue
			}
			if resolvedEntityID != "" {
				return "", "", ""
			}
			resolvedEntityID = entityID
		}
	}
	if resolvedEntityID == "" {
		return "", "", ""
	}
	return resolvedEntityID, ctx.index.entityFileByID[resolvedEntityID], codeprovenance.MethodImportBinding
}

func perlPackageQualifiedCall(call map[string]any) (string, string, bool) {
	fullName := strings.TrimSpace(anyToString(call["full_name"]))
	if fullName == "" {
		fullName = strings.TrimSpace(anyToString(call["name"]))
	}
	separator := strings.LastIndex(fullName, "::")
	if separator <= 0 || separator >= len(fullName)-2 {
		return "", "", false
	}
	packageName := strings.TrimSpace(fullName[:separator])
	methodName := strings.TrimSpace(fullName[separator+2:])
	return packageName, methodName, packageName != "" && methodName != ""
}

func perlFileImportsPackage(fileData map[string]any, packageName string) bool {
	packageName = strings.TrimSpace(packageName)
	if packageName == "" {
		return false
	}
	for _, entry := range mapSlice(fileData["imports"]) {
		if lang := strings.TrimSpace(anyToString(entry["lang"])); lang != "" && lang != "perl" {
			continue
		}
		if strings.TrimSpace(anyToString(entry["name"])) == packageName {
			return true
		}
	}
	return false
}

func perlImportedFunctionCandidateNames(packageName string, methodName string) []string {
	methodName = strings.TrimSpace(methodName)
	if methodName == "" {
		return nil
	}
	candidates := []string{strings.TrimSpace(packageName) + "::" + methodName, methodName}
	if receiver := codeCallTrailingName(packageName); receiver != "" {
		candidates = append(candidates, receiver+"."+methodName)
	}
	return candidates
}
