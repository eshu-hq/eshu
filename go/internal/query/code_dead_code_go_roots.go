package query

import (
	"slices"
	"strings"
)

type deadCodePolicyStats struct {
	RootsSkippedMissingSource    int
	ParserMetadataFrameworkRoots int
	SourceFallbackFrameworkRoots int
}

type deadCodeGoPolicyContext struct {
	language         string
	normalizedSource string
	rootKinds        []string
}

func newDeadCodeGoPolicyContext(result map[string]any, entity *EntityContent) deadCodeGoPolicyContext {
	return deadCodeGoPolicyContext{
		language:         strings.ToLower(deadCodeEntityLanguage(result, entity)),
		normalizedSource: deadCodeNormalizedSource(entity),
		rootKinds:        deadCodeRootKinds(result, entity),
	}
}

func deadCodeIsGoHTTPHandlerRoot(result map[string]any, policy deadCodeGoPolicyContext) bool {
	if policy.language != "go" {
		return false
	}
	if primaryEntityLabel(result) != "Function" {
		return false
	}
	if slices.Contains(policy.rootKinds, "go.net_http_handler_signature") ||
		slices.Contains(policy.rootKinds, "go.net_http_handler_registration") {
		return true
	}

	return strings.Contains(policy.normalizedSource, "http.responsewriter") &&
		strings.Contains(policy.normalizedSource, "*http.request")
}

func deadCodeIsGoCLICommandRoot(result map[string]any, policy deadCodeGoPolicyContext) bool {
	if policy.language != "go" {
		return false
	}
	if primaryEntityLabel(result) != "Function" {
		return false
	}
	if slices.Contains(policy.rootKinds, "go.cobra_run_signature") ||
		slices.Contains(policy.rootKinds, "go.cobra_run_registration") {
		return true
	}

	return strings.Contains(policy.normalizedSource, "*cobra.command") &&
		strings.Contains(policy.normalizedSource, "[]string")
}

func deadCodeIsGoFrameworkCallbackRoot(result map[string]any, policy deadCodeGoPolicyContext) bool {
	if policy.language != "go" {
		return false
	}
	if primaryEntityLabel(result) != "Function" {
		return false
	}
	if slices.Contains(policy.rootKinds, "go.controller_runtime_reconcile_signature") {
		return true
	}
	if strings.TrimSpace(StringVal(result, "name")) != "Reconcile" {
		return false
	}

	if !strings.Contains(policy.normalizedSource, "context.context") {
		return false
	}
	if !strings.Contains(policy.normalizedSource, "request") {
		return false
	}

	return (strings.Contains(policy.normalizedSource, "ctrl.request") || strings.Contains(policy.normalizedSource, "reconcile.request")) &&
		(strings.Contains(policy.normalizedSource, "ctrl.result") || strings.Contains(policy.normalizedSource, "reconcile.result"))
}

func deadCodeNormalizedSource(entity *EntityContent) string {
	if entity == nil {
		return ""
	}
	normalized := strings.ToLower(stripGoComments(entity.SourceCache))
	if normalized == "" {
		return ""
	}
	return strings.Join(strings.Fields(normalized), " ")
}

func deadCodeRootKinds(result map[string]any, entity *EntityContent) []string {
	if metadata, ok := result["metadata"].(map[string]any); ok {
		if kinds := deadCodeRootKindsFromMetadata(metadata); len(kinds) > 0 {
			return kinds
		}
	}
	if entity == nil {
		return nil
	}
	return deadCodeRootKindsFromMetadata(entity.Metadata)
}

func deadCodeRootKindsFromMetadata(metadata map[string]any) []string {
	if metadata == nil {
		return nil
	}
	raw, ok := metadata["dead_code_root_kinds"]
	if !ok {
		return nil
	}
	switch typed := raw.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		values := make([]string, 0, len(typed))
		for _, value := range typed {
			if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
				values = append(values, text)
			}
		}
		return values
	default:
		return nil
	}
}

func stripGoComments(source string) string {
	if source == "" {
		return ""
	}

	var out strings.Builder
	out.Grow(len(source))
	for i := 0; i < len(source); {
		switch {
		case i+1 < len(source) && source[i] == '/' && source[i+1] == '/':
			i += 2
			for i < len(source) && source[i] != '\n' {
				i++
			}
		case i+1 < len(source) && source[i] == '/' && source[i+1] == '*':
			i += 2
			for i+1 < len(source) && (source[i] != '*' || source[i+1] != '/') {
				i++
			}
			if i+1 < len(source) {
				i += 2
			} else {
				i = len(source)
			}
		default:
			out.WriteByte(source[i])
			i++
		}
	}
	return out.String()
}
