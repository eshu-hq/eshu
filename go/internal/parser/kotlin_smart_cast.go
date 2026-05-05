package parser

import (
	"regexp"
	"strings"
)

var (
	kotlinIfSmartCastPattern = regexp.MustCompile(
		`\bif\s*\(\s*([A-Za-z_]\w*)\s+is\s+([A-Za-z_]\w*(?:\.[A-Za-z_]\w*)*(?:<[^>]+>)?\??)`,
	)
	kotlinWhenSubjectPattern = regexp.MustCompile(`\bwhen\s*\(\s*([A-Za-z_]\w*)\s*\)`)
	kotlinWhenBranchPattern  = regexp.MustCompile(
		`^\s*is\s+([A-Za-z_]\w*(?:\.[A-Za-z_]\w*)*(?:<[^>]+>)?\??)\s*->`,
	)
)

type kotlinTypeFlowScope struct {
	functionName  string
	braceDepth    int
	variableTypes map[string]string
}

type kotlinWhenSubjectScope struct {
	functionName string
	braceDepth   int
	subject      string
}

func popKotlinTypeFlowScopes(scopes []kotlinTypeFlowScope, braceDepth int) []kotlinTypeFlowScope {
	for len(scopes) > 0 && braceDepth < scopes[len(scopes)-1].braceDepth {
		scopes = scopes[:len(scopes)-1]
	}
	return scopes
}

func popKotlinWhenSubjectScopes(scopes []kotlinWhenSubjectScope, braceDepth int) []kotlinWhenSubjectScope {
	for len(scopes) > 0 && braceDepth < scopes[len(scopes)-1].braceDepth {
		scopes = scopes[:len(scopes)-1]
	}
	return scopes
}

func kotlinMergeVariableTypes(base map[string]string, overlays ...map[string]string) map[string]string {
	merged := make(map[string]string, len(base))
	for name, typ := range base {
		merged[name] = typ
	}
	for _, overlay := range overlays {
		for name, typ := range overlay {
			if strings.TrimSpace(name) == "" || strings.TrimSpace(typ) == "" {
				continue
			}
			merged[name] = typ
		}
	}
	return merged
}

func kotlinActiveSmartCastTypes(scopes []kotlinTypeFlowScope, functionName string) map[string]string {
	if functionName == "" {
		return nil
	}
	active := make(map[string]string)
	for _, scope := range scopes {
		if scope.functionName != functionName {
			continue
		}
		for name, typ := range scope.variableTypes {
			active[name] = typ
		}
	}
	if len(active) == 0 {
		return nil
	}
	return active
}

func kotlinInlineSmartCastTypes(trimmed string, functionName string, whenScopes []kotlinWhenSubjectScope) map[string]string {
	inline := kotlinScopedSmartCastTypes(trimmed)
	subject := kotlinActiveWhenSubject(whenScopes, functionName)
	if subject == "" {
		return inline
	}

	matches := kotlinWhenBranchPattern.FindStringSubmatch(trimmed)
	if len(matches) != 2 {
		return inline
	}
	if inline == nil {
		inline = make(map[string]string, 1)
	}
	inline[subject] = kotlinCanonicalTypeReference(matches[1])
	return inline
}

func kotlinScopedSmartCastTypes(trimmed string) map[string]string {
	matches := kotlinIfSmartCastPattern.FindStringSubmatch(trimmed)
	if len(matches) != 3 {
		return nil
	}
	return map[string]string{
		matches[1]: kotlinCanonicalTypeReference(matches[2]),
	}
}

func kotlinWhenSubject(trimmed string) string {
	matches := kotlinWhenSubjectPattern.FindStringSubmatch(trimmed)
	if len(matches) != 2 {
		return ""
	}
	return strings.TrimSpace(matches[1])
}

func kotlinActiveWhenSubject(scopes []kotlinWhenSubjectScope, functionName string) string {
	for index := len(scopes) - 1; index >= 0; index-- {
		if scopes[index].functionName != functionName {
			continue
		}
		return scopes[index].subject
	}
	return ""
}
