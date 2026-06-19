package reducer

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

func init() {
	registerCodeCallLanguageResolvers(
		"groovy",
		codeCallLanguageResolver{
			phase:   codeCallLanguageResolverPhaseBeforeRepoFallback,
			resolve: resolveGroovyClassQualifiedCallee,
		},
	)
}

func resolveGroovyClassQualifiedCallee(ctx codeCallResolveContext) (string, string, codeprovenance.Method) {
	for _, candidateName := range groovyClassQualifiedCandidateNames(ctx.call) {
		entityID := ctx.index.uniqueNameByRepo[ctx.repositoryID][candidateName]
		if entityID == "" {
			continue
		}
		return entityID, ctx.index.entityFileByID[entityID], codeprovenance.MethodTypeInferred
	}
	return "", "", ""
}

func groovyClassQualifiedCandidateNames(call map[string]any) []string {
	callName := strings.TrimSpace(anyToString(call["name"]))
	receiverType := strings.TrimSpace(anyToString(call["inferred_obj_type"]))
	if callName == "" || receiverType == "" {
		return nil
	}
	candidates := []string{receiverType + "." + callName}
	if fullName := strings.TrimSpace(anyToString(call["full_name"])); strings.HasSuffix(fullName, "."+callName) {
		candidates = append(candidates, fullName)
	}
	return candidates
}
