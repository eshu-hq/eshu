package php

import (
	"regexp"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

type phpImportSpec struct {
	name       string
	alias      string
	importType string
}

func braceDelta(line string) int {
	delta := 0
	for _, r := range line {
		switch r {
		case '{':
			delta++
		case '}':
			delta--
		}
	}
	return delta
}

func parsePHPImports(raw string) []phpImportSpec {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}

	prefixKeyword := ""
	for _, keyword := range []string{"function ", "const "} {
		if strings.HasPrefix(strings.ToLower(trimmed), keyword) {
			prefixKeyword = strings.TrimSpace(keyword)
			trimmed = strings.TrimSpace(trimmed[len(keyword):])
			break
		}
	}
	importType := "use"
	if prefixKeyword != "" {
		importType = prefixKeyword
	}

	openBrace := strings.Index(trimmed, "{")
	closeBrace := strings.LastIndex(trimmed, "}")
	if openBrace < 0 || closeBrace <= openBrace {
		name, alias := parsePHPSingleImport(trimmed, prefixKeyword)
		if name == "" {
			return nil
		}
		return []phpImportSpec{{name: name, alias: alias, importType: importType}}
	}

	prefix := strings.TrimSpace(trimmed[:openBrace])
	prefix = strings.TrimSuffix(prefix, `\`)
	body := trimmed[openBrace+1 : closeBrace]
	clauses := splitPHPCommaSeparated(body)
	imports := make([]phpImportSpec, 0, len(clauses))
	for _, clause := range clauses {
		clause = strings.TrimSpace(clause)
		if clause == "" {
			continue
		}
		name, alias := parsePHPSingleImport(prefix+`\`+clause, prefixKeyword)
		if name == "" {
			continue
		}
		imports = append(imports, phpImportSpec{name: name, alias: alias, importType: importType})
	}
	return imports
}

func phpSemanticKindForMethod(name string) string {
	if strings.HasPrefix(name, "__") {
		return "magic_method"
	}
	return ""
}

func parsePHPSingleImport(raw string, prefixKeyword string) (string, string) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", ""
	}
	parts := strings.Fields(trimmed)
	if len(parts) == 0 {
		return "", ""
	}
	name := strings.TrimSpace(parts[0])
	if prefixKeyword != "" {
		name = strings.TrimSpace(prefixKeyword + " " + name)
	}
	alias := ""
	if len(parts) >= 3 && strings.EqualFold(parts[1], "as") {
		alias = strings.TrimSpace(parts[2])
	}
	if prefixKeyword != "" {
		name = strings.TrimSpace(strings.TrimPrefix(name, prefixKeyword+" "))
	}
	return name, alias
}

func splitPHPCommaSeparated(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := make([]string, 0, 4)
	var current strings.Builder
	braceDepth := 0
	for _, r := range raw {
		switch r {
		case '{':
			braceDepth++
			current.WriteRune(r)
		case '}':
			if braceDepth > 0 {
				braceDepth--
			}
			current.WriteRune(r)
		case ',':
			if braceDepth == 0 {
				parts = append(parts, strings.TrimSpace(current.String()))
				current.Reset()
				continue
			}
			current.WriteRune(r)
		default:
			current.WriteRune(r)
		}
	}
	if tail := strings.TrimSpace(current.String()); tail != "" {
		parts = append(parts, tail)
	}
	return parts
}

func parsePHPBases(kind string, tail string) []string {
	remaining := strings.TrimSpace(tail)
	if remaining == "" {
		return nil
	}

	remaining = strings.TrimSpace(strings.TrimSuffix(remaining, "{"))
	if remaining == "" {
		return nil
	}

	var rawBases string
	switch kind {
	case "class":
		if index := strings.Index(remaining, "extends"); index >= 0 {
			afterExtends := strings.TrimSpace(remaining[index+len("extends"):])
			if split := strings.Index(afterExtends, "implements"); split >= 0 {
				extendsBases := appendPHPBaseList(strings.TrimSpace(afterExtends[:split]))
				implementsBases := appendPHPBaseList(strings.TrimSpace(afterExtends[split+len("implements"):]))
				return append(extendsBases, implementsBases...)
			}
			rawBases = afterExtends
		}
		if index := strings.Index(remaining, "implements"); index >= 0 {
			rawBases = strings.TrimSpace(remaining[index+len("implements"):])
		}
	case "interface":
		if index := strings.Index(remaining, "extends"); index >= 0 {
			rawBases = strings.TrimSpace(remaining[index+len("extends"):])
		}
	}

	if rawBases == "" {
		return nil
	}
	return appendPHPBaseList(rawBases)
}

func appendPHPBaseList(raw string) []string {
	segments := strings.Split(raw, ",")
	bases := make([]string, 0, len(segments))
	for _, segment := range segments {
		trimmed := strings.TrimSpace(segment)
		if trimmed == "" {
			continue
		}
		bases = append(bases, shared.LastPathSegment(trimmed, `\`))
	}
	return bases
}

func parsePHPClassTraitUses(raw string) []string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || !strings.HasPrefix(trimmed, "use ") {
		return nil
	}
	body := strings.TrimSpace(trimmed[len("use "):])
	if body == "" {
		return nil
	}
	if braceIndex := strings.Index(body, "{"); braceIndex >= 0 {
		body = strings.TrimSpace(body[:braceIndex])
	}
	if semicolonIndex := strings.Index(body, ";"); semicolonIndex >= 0 {
		body = strings.TrimSpace(body[:semicolonIndex])
	}
	if body == "" {
		return nil
	}
	if body == "" {
		return nil
	}
	segments := strings.Split(body, ",")
	traits := make([]string, 0, len(segments))
	for _, segment := range segments {
		trait := strings.TrimSpace(segment)
		if trait == "" {
			continue
		}
		traits = append(traits, shared.LastPathSegment(trait, `\`))
	}
	return dedupePHPNonEmptyStrings(traits)
}

func appendPHPClassBases(payload map[string]any, className string, additionalBases []string) {
	if className == "" || len(additionalBases) == 0 {
		return
	}
	items, _ := payload["classes"].([]map[string]any)
	for _, item := range items {
		name, _ := item["name"].(string)
		if name != className {
			continue
		}
		existing, _ := item["bases"].([]string)
		merged := dedupePHPNonEmptyStrings(append(existing, additionalBases...))
		if len(merged) > 0 {
			item["bases"] = merged
		}
		return
	}
}

func extractPHPParameters(lines []string, startIndex int, rawLine string) []string {
	signature := rawLine
	for index := startIndex; index < len(lines) && !strings.Contains(signature, ")"); index++ {
		if index == startIndex {
			continue
		}
		signature += " " + strings.TrimSpace(lines[index])
	}
	start := strings.Index(signature, "(")
	end := strings.LastIndex(signature, ")")
	if start < 0 || end < 0 || end <= start {
		return nil
	}
	rawParams := signature[start+1 : end]
	if strings.TrimSpace(rawParams) == "" {
		return []string{}
	}
	parts := strings.Split(rawParams, ",")
	parameters := make([]string, 0, len(parts))
	for _, part := range parts {
		match := regexp.MustCompile(`\$(\w+)`).FindStringSubmatch(part)
		if len(match) != 2 {
			continue
		}
		parameters = append(parameters, "$"+match[1])
	}
	return parameters
}

func collectPHPBlockSource(lines []string, startIndex int) string {
	if startIndex < 0 || startIndex >= len(lines) {
		return ""
	}

	var builder strings.Builder
	depth := 0
	sawOpenBrace := false
	for index := startIndex; index < len(lines); index++ {
		if index > startIndex {
			builder.WriteString("\n")
		}
		builder.WriteString(lines[index])
		if strings.Contains(lines[index], "{") {
			sawOpenBrace = true
		}
		depth += braceDelta(lines[index])
		if sawOpenBrace && depth <= 0 {
			break
		}
	}
	return builder.String()
}

func currentPHPContext(stack []phpScopedContext) (string, string, int) {
	for index := len(stack) - 1; index >= 0; index-- {
		switch stack[index].kind {
		case "function_definition", "method_declaration", "class_declaration", "interface_declaration", "trait_declaration":
			return stack[index].name, stack[index].kind, stack[index].lineNumber
		}
	}
	return "", "", 0
}

func extractPHPCallArgs(lines []string, startIndex int, rawLine string, callText string) []string {
	start := strings.Index(rawLine, callText)
	if start < 0 {
		return nil
	}
	remainder := rawLine[start+len(callText):]
	if startIndex+1 < len(lines) {
		remainder += "\n" + strings.Join(lines[startIndex+1:], "\n")
	}
	rawArgs, ok := collectPHPCallArgumentSource(remainder)
	if !ok {
		return nil
	}
	rawArgs = strings.TrimSpace(rawArgs)
	if rawArgs == "" {
		return []string{}
	}
	return splitPHPCallArguments(rawArgs)
}

func collectPHPCallArgumentSource(raw string) (string, bool) {
	if raw == "" {
		return "", false
	}

	depth := 1
	inSingleQuote := false
	inDoubleQuote := false
	escaped := false
	var builder strings.Builder

	for index := 0; index < len(raw); index++ {
		ch := raw[index]
		if escaped {
			builder.WriteByte(ch)
			escaped = false
			continue
		}
		if inSingleQuote {
			builder.WriteByte(ch)
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '\'' {
				inSingleQuote = false
			}
			continue
		}
		if inDoubleQuote {
			builder.WriteByte(ch)
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inDoubleQuote = false
			}
			continue
		}

		switch ch {
		case '\'':
			inSingleQuote = true
			builder.WriteByte(ch)
		case '"':
			inDoubleQuote = true
			builder.WriteByte(ch)
		case '(':
			depth++
			builder.WriteByte(ch)
		case ')':
			depth--
			if depth == 0 {
				return builder.String(), true
			}
			builder.WriteByte(ch)
		default:
			builder.WriteByte(ch)
		}
	}

	return "", false
}

func splitPHPCallArguments(raw string) []string {
	args := make([]string, 0)
	var current strings.Builder
	parenDepth := 0
	bracketDepth := 0
	braceDepth := 0
	inSingleQuote := false
	inDoubleQuote := false
	escaped := false

	flush := func() {
		trimmed := strings.TrimSpace(current.String())
		if trimmed != "" {
			args = append(args, trimmed)
		}
		current.Reset()
	}

	for index := 0; index < len(raw); index++ {
		ch := raw[index]
		if escaped {
			current.WriteByte(ch)
			escaped = false
			continue
		}
		if inSingleQuote {
			current.WriteByte(ch)
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '\'' {
				inSingleQuote = false
			}
			continue
		}
		if inDoubleQuote {
			current.WriteByte(ch)
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inDoubleQuote = false
			}
			continue
		}

		switch ch {
		case '\'':
			inSingleQuote = true
			current.WriteByte(ch)
		case '"':
			inDoubleQuote = true
			current.WriteByte(ch)
		case '(':
			parenDepth++
			current.WriteByte(ch)
		case ')':
			if parenDepth > 0 {
				parenDepth--
			}
			current.WriteByte(ch)
		case '[':
			bracketDepth++
			current.WriteByte(ch)
		case ']':
			if bracketDepth > 0 {
				bracketDepth--
			}
			current.WriteByte(ch)
		case '{':
			braceDepth++
			current.WriteByte(ch)
		case '}':
			if braceDepth > 0 {
				braceDepth--
			}
			current.WriteByte(ch)
		case ',':
			if parenDepth == 0 && bracketDepth == 0 && braceDepth == 0 {
				flush()
				continue
			}
			current.WriteByte(ch)
		default:
			current.WriteByte(ch)
		}
	}

	flush()
	return args
}
