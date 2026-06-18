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
