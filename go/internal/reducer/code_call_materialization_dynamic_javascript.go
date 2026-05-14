package reducer

import (
	"path/filepath"
	"regexp"
	"strings"
)

var (
	javaScriptStaticStringVarRe = regexp.MustCompile(`\b(?:const|let|var)\s+([A-Za-z_$][A-Za-z0-9_$]*)\s*=\s*["']([^"']+)["']`)
	javaScriptStaticObjectRe    = regexp.MustCompile(`(?s)\b(?:const|let|var)\s+([A-Za-z_$][A-Za-z0-9_$]*)\s*=\s*\{(.*?)\}`)
	javaScriptObjectEntryRe     = regexp.MustCompile(`(?:^|,)\s*(?:([A-Za-z_$][A-Za-z0-9_$]*)|["']([^"']+)["'])\s*:\s*([A-Za-z_$][A-Za-z0-9_$]*(?:\.[A-Za-z_$][A-Za-z0-9_$]*)*)`)
	javaScriptDestructureRe     = regexp.MustCompile(`(?s)\b(?:const|let|var)\s*\{(.*?)\}\s*=\s*([A-Za-z_$][A-Za-z0-9_$]*)`)
	javaScriptDestructureItemRe = regexp.MustCompile(`(?:^|,)\s*([A-Za-z_$][A-Za-z0-9_$]*)\s*(?::\s*([A-Za-z_$][A-Za-z0-9_$]*))?`)
	javaScriptStringMemberRe    = regexp.MustCompile(`\[\s*["']([^"']+)["']\s*\]`)
	javaScriptVariableMemberRe  = regexp.MustCompile(`\[\s*([A-Za-z_$][A-Za-z0-9_$]*)\s*\]`)
)

type javaScriptStaticAliasSet struct {
	aliases       map[string]string
	staticStrings map[string]string
	scanned       bool
}

// resolveDynamicJavaScriptCalleeEntityID handles static JavaScript patterns
// that look dynamic in call metadata but have a literal same-file target.
func resolveDynamicJavaScriptCalleeEntityID(
	index codeEntityIndex,
	rawPath string,
	relativePath string,
	fileData map[string]any,
	call map[string]any,
) string {
	if !codeCallJavaScriptFamily(codeCallLanguage(call, rawPath, relativePath)) {
		return ""
	}
	callLine := codeCallInt(call["line_number"], call["ref_line"])
	if callLine <= 0 {
		return ""
	}

	// JavaScript alias metadata should normally come from the index; the
	// fallback preserves direct helper tests that bypass index construction.
	aliasSet, ok := javaScriptStaticAliasesForCall(index, rawPath, relativePath, callLine)
	if !ok {
		source := javaScriptContainingFunctionSource(fileData, callLine)
		if strings.TrimSpace(source) == "" {
			return ""
		}
		aliases, staticStrings := javaScriptStaticAliases(source)
		aliasSet = javaScriptStaticAliasSet{
			aliases:       aliases,
			staticStrings: staticStrings,
			scanned:       true,
		}
	}
	for _, candidate := range javaScriptDynamicCallCandidates(call, aliasSet.staticStrings) {
		if strings.ContainsAny(candidate, "[]") {
			continue
		}
		target := candidate
		if aliasTarget := aliasSet.aliases[candidate]; aliasTarget != "" {
			target = aliasTarget
		}
		if entityID := resolveSameFileJavaScriptDynamicTarget(index, rawPath, relativePath, target); entityID != "" {
			return entityID
		}
	}
	return ""
}

func codeCallJavaScriptSourceFile(fileData map[string]any, rawPath string, relativePath string) bool {
	language := anyToString(fileData["language"])
	if language == "" {
		language = anyToString(fileData["lang"])
	}
	if codeCallJavaScriptFamily(language) {
		return true
	}
	switch strings.ToLower(filepath.Ext(codeCallPreferredPath(rawPath, relativePath))) {
	case ".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs", ".mts", ".cts":
		return true
	default:
		return false
	}
}

func cacheJavaScriptStaticAliasSpan(
	index codeEntityIndex,
	pathKeys []string,
	startLine int,
	endLine int,
	source string,
) {
	if len(pathKeys) == 0 || startLine <= 0 || strings.TrimSpace(source) == "" {
		return
	}
	aliases, staticStrings := javaScriptStaticAliases(source)
	aliasSet := javaScriptStaticAliasSet{
		aliases:       aliases,
		staticStrings: staticStrings,
		scanned:       true,
	}
	for _, pathKey := range pathKeys {
		if pathKey == "" {
			continue
		}
		index.javaScriptAliasesByPath[pathKey] = append(
			index.javaScriptAliasesByPath[pathKey],
			javaScriptStaticAliasSpan{
				startLine: startLine,
				endLine:   endLine,
				aliases:   aliasSet,
			},
		)
	}
}

func javaScriptStaticAliasesForCall(
	index codeEntityIndex,
	rawPath string,
	relativePath string,
	line int,
) (javaScriptStaticAliasSet, bool) {
	var (
		bestAlias javaScriptStaticAliasSet
		bestWidth int
	)
	for _, pathKey := range codeCallPathKeys(rawPath, relativePath) {
		for _, span := range index.javaScriptAliasesByPath[pathKey] {
			if line < span.startLine || line > span.endLine {
				continue
			}
			width := span.endLine - span.startLine
			if !bestAlias.scanned || width < bestWidth {
				bestAlias = span.aliases
				bestWidth = width
			}
		}
		if bestAlias.scanned {
			return bestAlias, true
		}
	}
	return javaScriptStaticAliasSet{}, false
}

func javaScriptContainingFunctionSource(fileData map[string]any, line int) string {
	if line <= 0 {
		return ""
	}
	var source string
	bestWidth := 0
	for _, item := range mapSlice(fileData["functions"]) {
		startLine := codeCallInt(item["line_number"], item["start_line"])
		endLine := codeCallInt(item["end_line"])
		if startLine <= 0 || line < startLine {
			continue
		}
		if endLine < startLine {
			endLine = startLine
		}
		if line > endLine {
			continue
		}
		width := endLine - startLine
		if source == "" || width < bestWidth {
			source = anyToString(item["source"])
			bestWidth = width
		}
	}
	return source
}

func javaScriptStaticAliases(source string) (map[string]string, map[string]string) {
	aliases := make(map[string]string)
	staticStrings := make(map[string]string)
	for _, match := range javaScriptStaticStringVarRe.FindAllStringSubmatch(source, -1) {
		if len(match) == 3 {
			staticStrings[match[1]] = match[2]
		}
	}

	for _, match := range javaScriptStaticObjectRe.FindAllStringSubmatch(source, -1) {
		if len(match) != 3 {
			continue
		}
		objectName := match[1]
		body := match[2]
		for _, entry := range javaScriptObjectEntryRe.FindAllStringSubmatch(body, -1) {
			if len(entry) != 4 {
				continue
			}
			key := entry[1]
			if key == "" {
				key = entry[2]
			}
			target := entry[3]
			if key == "" || target == "" {
				continue
			}
			aliases[objectName+"."+key] = target
		}
	}

	for _, match := range javaScriptDestructureRe.FindAllStringSubmatch(source, -1) {
		if len(match) != 3 {
			continue
		}
		body := match[1]
		objectName := match[2]
		for _, item := range javaScriptDestructureItemRe.FindAllStringSubmatch(body, -1) {
			if len(item) != 3 {
				continue
			}
			key := item[1]
			local := item[2]
			if local == "" {
				local = key
			}
			if target := aliases[objectName+"."+key]; target != "" {
				aliases[local] = target
			}
		}
	}

	return aliases, staticStrings
}

func javaScriptDynamicCallCandidates(call map[string]any, staticStrings map[string]string) []string {
	candidates := make([]string, 0, 6)
	appendCandidate := func(value string) {
		value = javaScriptNormalizeStaticMemberExpression(value, staticStrings)
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		for _, existing := range candidates {
			if existing == value {
				return
			}
		}
		candidates = append(candidates, value)
	}

	fullName := anyToString(call["full_name"])
	appendCandidate(fullName)
	appendCandidate(anyToString(call["name"]))
	for _, receiver := range codeCallJavaScriptFunctionReceiverNames(fullName) {
		appendCandidate(receiver)
	}
	return candidates
}

func javaScriptNormalizeStaticMemberExpression(value string, staticStrings map[string]string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = javaScriptStringMemberRe.ReplaceAllString(value, ".$1")
	return javaScriptVariableMemberRe.ReplaceAllStringFunc(value, func(match string) string {
		parts := javaScriptVariableMemberRe.FindStringSubmatch(match)
		if len(parts) != 2 {
			return match
		}
		if resolved := staticStrings[parts[1]]; resolved != "" {
			return "." + resolved
		}
		return match
	})
}

func resolveSameFileJavaScriptDynamicTarget(
	index codeEntityIndex,
	rawPath string,
	relativePath string,
	target string,
) string {
	target = strings.TrimSpace(target)
	if target == "" || strings.ContainsAny(target, "[]") {
		return ""
	}
	candidates := []string{target, codeCallTrailingName(target)}
	for _, pathKey := range codeCallPathKeys(rawPath, relativePath) {
		for _, candidate := range candidates {
			if entityID := index.uniqueNameByPath[pathKey][candidate]; entityID != "" {
				return entityID
			}
		}
	}
	return ""
}
