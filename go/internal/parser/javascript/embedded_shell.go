package javascript

import (
	"regexp"
	"sort"
	"strings"
)

var (
	jsFunctionPattern           = regexp.MustCompile(`function\s+(?P<name>[A-Za-z_$][\w$]*)\s*\([^)]*\)\s*\{`)
	jsRequireAliasPattern       = regexp.MustCompile(`(?m)\b(?:const|let|var)\s+(?P<alias>[A-Za-z_$][\w$]*)\s*=\s*require\(\s*["'](?:node:)?child_process["']\s*\)`)
	jsRequireDestructurePattern = regexp.MustCompile(`(?m)\b(?:const|let|var)\s*\{(?P<names>[^}]+)\}\s*=\s*require\(\s*["'](?:node:)?child_process["']\s*\)`)
	jsImportNamespacePattern    = regexp.MustCompile(`(?m)\bimport\s+\*\s+as\s+(?P<alias>[A-Za-z_$][\w$]*)\s+from\s+["'](?:node:)?child_process["']`)
	jsImportDefaultPattern      = regexp.MustCompile(`(?m)\bimport\s+(?P<alias>[A-Za-z_$][\w$]*)\s+from\s+["'](?:node:)?child_process["']`)
	jsImportNamedPattern        = regexp.MustCompile(`(?m)\bimport\s*\{(?P<names>[^}]+)\}\s+from\s+["'](?:node:)?child_process["']`)
	jsChildProcessCallPattern   = regexp.MustCompile(`\b(?P<alias>[A-Za-z_$][\w$]*)\s*\.\s*(?P<call>execFileSync|execFile|execSync|exec|spawnSync|spawn|fork)\s*\(`)
)

var jsChildProcessCalls = map[string]struct{}{
	"exec":         {},
	"execSync":     {},
	"execFile":     {},
	"execFileSync": {},
	"spawn":        {},
	"spawnSync":    {},
	"fork":         {},
}

type jsEmbeddedShellCommand struct {
	functionName       string
	functionLineNumber int
	lineNumber         int
	api                string
	language           string
}

type jsShellImports struct {
	moduleAliases map[string]struct{}
	directCalls   map[string]string
}

type jsFunctionBody struct {
	name        string
	body        string
	startOffset int
	lineNumber  int
}

func embeddedShellCommandPayloads(source string, language string) []map[string]any {
	commands := embeddedShellCommands(source, language)
	if len(commands) == 0 {
		return []map[string]any{}
	}
	payload := make([]map[string]any, 0, len(commands))
	for _, command := range commands {
		payload = append(payload, map[string]any{
			"function_name":        command.functionName,
			"function_line_number": command.functionLineNumber,
			"line_number":          command.lineNumber,
			"api":                  command.api,
			"language":             command.language,
		})
	}
	return payload
}

func embeddedShellCommands(source string, language string) []jsEmbeddedShellCommand {
	imports := jsShellImportAliases(source)
	if len(imports.moduleAliases) == 0 && len(imports.directCalls) == 0 {
		return nil
	}

	var commands []jsEmbeddedShellCommand
	for _, function := range iterJSFunctionBodies(source) {
		for _, match := range jsChildProcessCallPattern.FindAllStringSubmatchIndex(function.body, -1) {
			aliasIndex := jsChildProcessCallPattern.SubexpIndex("alias")
			callIndex := jsChildProcessCallPattern.SubexpIndex("call")
			alias := function.body[match[2*aliasIndex]:match[2*aliasIndex+1]]
			if _, ok := imports.moduleAliases[alias]; !ok {
				continue
			}
			if jsIdentifierShadowedBefore(function.body[:match[0]], alias) {
				continue
			}
			callName := function.body[match[2*callIndex]:match[2*callIndex+1]]
			commands = append(commands, jsEmbeddedShellCommand{
				functionName:       function.name,
				functionLineNumber: function.lineNumber,
				lineNumber:         lineNumberForOffset(source, function.startOffset+match[0]),
				api:                "child_process." + callName,
				language:           language,
			})
		}
		for local, api := range imports.directCalls {
			for _, match := range jsIdentifierCallPattern(local).FindAllStringIndex(function.body, -1) {
				if jsIdentifierShadowedBefore(function.body[:match[0]], local) {
					continue
				}
				commands = append(commands, jsEmbeddedShellCommand{
					functionName:       function.name,
					functionLineNumber: function.lineNumber,
					lineNumber:         lineNumberForOffset(source, function.startOffset+match[0]),
					api:                api,
					language:           language,
				})
			}
		}
	}
	sort.Slice(commands, func(i, j int) bool {
		if commands[i].lineNumber != commands[j].lineNumber {
			return commands[i].lineNumber < commands[j].lineNumber
		}
		return commands[i].api < commands[j].api
	})
	return commands
}

func jsShellImportAliases(source string) jsShellImports {
	imports := jsShellImports{moduleAliases: map[string]struct{}{}, directCalls: map[string]string{}}
	for _, pattern := range []*regexp.Regexp{jsRequireAliasPattern, jsImportNamespacePattern, jsImportDefaultPattern} {
		for _, match := range pattern.FindAllStringSubmatch(source, -1) {
			imports.moduleAliases[strings.TrimSpace(match[pattern.SubexpIndex("alias")])] = struct{}{}
		}
	}
	for _, pattern := range []*regexp.Regexp{jsRequireDestructurePattern, jsImportNamedPattern} {
		for _, match := range pattern.FindAllStringSubmatch(source, -1) {
			for local, imported := range jsNamedImports(match[pattern.SubexpIndex("names")]) {
				if _, ok := jsChildProcessCalls[imported]; ok {
					imports.directCalls[local] = "child_process." + imported
				}
			}
		}
	}
	return imports
}

func jsNamedImports(raw string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(raw, ",") {
		segment := strings.TrimSpace(part)
		if segment == "" {
			continue
		}
		if strings.Contains(segment, ":") {
			pair := strings.SplitN(segment, ":", 2)
			out[strings.TrimSpace(pair[1])] = strings.TrimSpace(pair[0])
			continue
		}
		fields := strings.Fields(segment)
		if len(fields) == 3 && fields[1] == "as" {
			out[fields[2]] = fields[0]
			continue
		}
		out[segment] = segment
	}
	return out
}

func iterJSFunctionBodies(source string) []jsFunctionBody {
	matches := jsFunctionPattern.FindAllStringSubmatchIndex(source, -1)
	nameIndex := jsFunctionPattern.SubexpIndex("name")
	bodies := make([]jsFunctionBody, 0, len(matches))
	for _, match := range matches {
		openBrace := strings.IndexByte(source[match[0]:], '{')
		if openBrace < 0 {
			continue
		}
		openIndex := match[0] + openBrace
		closeIndex := matchingJSBraceIndex(source, openIndex)
		if closeIndex < 0 {
			continue
		}
		bodyStart := openIndex + 1
		bodies = append(bodies, jsFunctionBody{
			name:        source[match[2*nameIndex]:match[2*nameIndex+1]],
			body:        source[bodyStart:closeIndex],
			startOffset: bodyStart,
			lineNumber:  lineNumberForOffset(source, match[0]),
		})
	}
	return bodies
}

func matchingJSBraceIndex(source string, openIndex int) int {
	depth := 0
	for index := openIndex; index < len(source); index++ {
		switch source[index] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return index
			}
		}
	}
	return -1
}

func jsIdentifierShadowedBefore(source string, identifier string) bool {
	pattern := regexp.MustCompile(`\b(?:const|let|var)?\s*` + regexp.QuoteMeta(identifier) + `\s*=`)
	return pattern.MatchString(source)
}

func jsIdentifierCallPattern(identifier string) *regexp.Regexp {
	return regexp.MustCompile(`\b` + regexp.QuoteMeta(identifier) + `\s*\(`)
}

func lineNumberForOffset(source string, offset int) int {
	return strings.Count(source[:offset], "\n") + 1
}
