package reducer

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

func init() {
	registerCodeCallLanguageResolvers(
		"java",
		codeCallLanguageResolver{
			phase:   codeCallLanguageResolverPhaseBeforeRepoFallback,
			resolve: resolveJavaSemanticCallee,
		},
	)
}

func resolveJavaSemanticCallee(ctx codeCallResolveContext) (string, string, codeprovenance.Method) {
	if entityID := resolveJavaImportedReceiverCallee(ctx); entityID != "" {
		return entityID, ctx.index.entityFileByID[entityID], codeprovenance.MethodImportBinding
	}
	for _, candidateName := range javaSemanticCandidateNames(ctx.call) {
		entityID := resolveJavaSemanticCandidate(ctx, candidateName)
		if entityID == "" {
			continue
		}
		return entityID, ctx.index.entityFileByID[entityID], codeprovenance.MethodTypeInferred
	}
	return "", "", ""
}

func javaSemanticCandidateNames(call map[string]any) []string {
	receiverType := strings.TrimSpace(anyToString(call["inferred_obj_type"]))
	callName := strings.TrimSpace(anyToString(call["name"]))
	if receiverType == "" || callName == "" {
		return nil
	}
	candidates := []string{receiverType + "." + callName}
	if argumentTypes := codeCallMetadataStringSlice(call, "argument_types"); len(argumentTypes) > 0 {
		candidates = codeCallAppendTypedSignatureNames(candidates, argumentTypes)
	}
	if arity, ok := codeCallMetadataInt(call, "argument_count"); ok {
		candidates = codeCallAppendArityNames(candidates, arity)
	}
	return candidates
}

func resolveJavaSemanticCandidate(ctx codeCallResolveContext, candidateName string) string {
	candidateName = strings.TrimSpace(candidateName)
	if candidateName == "" || ctx.repositoryID == "" {
		return ""
	}
	callerDir := codeCallDirectoryKey(codeCallPreferredPath(ctx.rawPath, ctx.relativePath))
	if callerDir != "" {
		if entityID := ctx.index.uniqueNameByRepoDir[ctx.repositoryID][callerDir][candidateName]; entityID != "" {
			return entityID
		}
	}
	return ctx.index.uniqueNameByRepo[ctx.repositoryID][candidateName]
}

func resolveJavaImportedReceiverCallee(ctx codeCallResolveContext) string {
	if ctx.repositoryID == "" {
		return ""
	}
	paths := javaImportedReceiverPaths(ctx)
	if len(paths) == 0 {
		return ""
	}
	var resolvedEntityID string
	for _, candidateName := range javaSemanticCandidateNames(ctx.call) {
		for _, path := range paths {
			entityID := ctx.index.uniqueNameByPath[path][candidateName]
			if entityID == "" || entityID == resolvedEntityID {
				continue
			}
			if resolvedEntityID != "" {
				return ""
			}
			resolvedEntityID = entityID
		}
	}
	return resolvedEntityID
}

func javaImportedReceiverPaths(ctx codeCallResolveContext) []string {
	receiverType := strings.TrimSpace(anyToString(ctx.call["inferred_obj_type"]))
	if receiverType == "" {
		return nil
	}
	importEntries := mapSlice(ctx.fileData["imports"])
	pathsByReceiver := ctx.repositoryImports[receiverType]
	if len(importEntries) == 0 || len(pathsByReceiver) == 0 {
		return nil
	}

	var paths []string
	appendPath := func(path string) {
		path = normalizeCodeCallPath(path)
		if path == "" {
			return
		}
		for _, existing := range paths {
			if existing == path {
				return
			}
		}
		paths = append(paths, path)
	}

	for _, entry := range importEntries {
		if !javaImportEntryMatchesReceiver(entry, receiverType) {
			continue
		}
		for _, source := range codeCallImportEntrySources(entry) {
			for _, path := range pathsByReceiver {
				if javaImportSourceMatchesPath(source, receiverType, path) {
					appendPath(path)
				}
			}
		}
	}
	return paths
}

func javaImportEntryMatchesReceiver(entry map[string]any, receiverType string) bool {
	receiverType = strings.TrimSpace(receiverType)
	if receiverType == "" || strings.TrimSpace(anyToString(entry["import_type"])) != "import" {
		return false
	}
	for _, value := range []string{
		anyToString(entry["alias"]),
		codeCallTrailingName(anyToString(entry["name"])),
		codeCallTrailingName(anyToString(entry["source"])),
	} {
		if strings.TrimSpace(value) == receiverType {
			return true
		}
	}
	source := strings.TrimSpace(anyToString(entry["source"]))
	return strings.HasSuffix(source, ".*")
}

func javaImportSourceMatchesPath(source string, receiverType string, path string) bool {
	source = strings.TrimSpace(source)
	receiverType = strings.TrimSpace(receiverType)
	path = normalizeCodeCallPath(path)
	if source == "" || receiverType == "" || path == "" {
		return false
	}
	if strings.HasSuffix(source, ".*") {
		source = strings.TrimSuffix(source, ".*") + "." + receiverType
	}
	sourcePath := strings.ReplaceAll(source, ".", "/") + ".java"
	return strings.HasSuffix(path, sourcePath)
}
