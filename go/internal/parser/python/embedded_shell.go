package python

import (
	"regexp"
	"sort"
	"strings"
)

var (
	pythonImportSubprocessPattern = regexp.MustCompile(`(?m)^\s*import\s+subprocess(?:\s+as\s+(?P<alias>[A-Za-z_]\w*))?`)
	pythonImportOSPattern         = regexp.MustCompile(`(?m)^\s*import\s+os(?:\s+as\s+(?P<alias>[A-Za-z_]\w*))?`)
	pythonFromSubprocessPattern   = regexp.MustCompile(`(?m)^\s*from\s+subprocess\s+import\s+(?P<names>[^\n]+)`)
	pythonFromOSPattern           = regexp.MustCompile(`(?m)^\s*from\s+os\s+import\s+(?P<names>[^\n]+)`)
	pythonDefPattern              = regexp.MustCompile(`^(?P<indent>\s*)(?:async\s+)?def\s+(?P<name>[A-Za-z_]\w*)\s*\(`)
)

var pythonSubprocessCalls = map[string]struct{}{
	"Popen":        {},
	"run":          {},
	"call":         {},
	"check_call":   {},
	"check_output": {},
}

type embeddedShellCommand struct {
	functionName       string
	functionLineNumber int
	lineNumber         int
	api                string
	language           string
}

type pythonShellImports struct {
	moduleAliases map[string]string
	directCalls   map[string]string
}

func embeddedShellCommandPayloads(source string) []map[string]any {
	commands := embeddedShellCommands(source)
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

func embeddedShellCommands(source string) []embeddedShellCommand {
	imports := pythonShellImportAliases(source)
	if len(imports.moduleAliases) == 0 && len(imports.directCalls) == 0 {
		return nil
	}

	lines := strings.Split(source, "\n")
	var commands []embeddedShellCommand
	for index := 0; index < len(lines); index++ {
		match := pythonDefPattern.FindStringSubmatch(lines[index])
		if match == nil {
			continue
		}
		name := match[pythonDefPattern.SubexpIndex("name")]
		indent := len(match[pythonDefPattern.SubexpIndex("indent")])
		startLine := index + 1
		bodyStart := index + 1
		bodyEnd := pythonFunctionBodyEnd(lines, bodyStart, indent)
		bodyLines := lines[bodyStart:bodyEnd]
		for offset, line := range bodyLines {
			lineNumber := bodyStart + offset + 1
			for alias, module := range imports.moduleAliases {
				if pythonIdentifierShadowedBefore(bodyLines[:offset], alias) {
					continue
				}
				for callName := range pythonSubprocessCalls {
					api := module + "." + callName
					if module == "os" {
						callName = "system"
						api = "os.system"
					}
					if pythonLineCalls(line, alias+"."+callName) {
						commands = append(commands, embeddedShellCommand{name, startLine, lineNumber, api, "python"})
					}
					if module == "os" {
						break
					}
				}
			}
			for callName, api := range imports.directCalls {
				if pythonIdentifierShadowedBefore(bodyLines[:offset], callName) {
					continue
				}
				if pythonLineCalls(line, callName) {
					commands = append(commands, embeddedShellCommand{name, startLine, lineNumber, api, "python"})
				}
			}
		}
		index = bodyEnd - 1
	}
	sort.Slice(commands, func(i, j int) bool {
		if commands[i].lineNumber != commands[j].lineNumber {
			return commands[i].lineNumber < commands[j].lineNumber
		}
		return commands[i].api < commands[j].api
	})
	return commands
}

func pythonShellImportAliases(source string) pythonShellImports {
	imports := pythonShellImports{moduleAliases: map[string]string{}, directCalls: map[string]string{}}
	for _, match := range pythonImportSubprocessPattern.FindAllStringSubmatch(source, -1) {
		alias := strings.TrimSpace(match[pythonImportSubprocessPattern.SubexpIndex("alias")])
		if alias == "" {
			alias = "subprocess"
		}
		imports.moduleAliases[alias] = "subprocess"
	}
	for _, match := range pythonImportOSPattern.FindAllStringSubmatch(source, -1) {
		alias := strings.TrimSpace(match[pythonImportOSPattern.SubexpIndex("alias")])
		if alias == "" {
			alias = "os"
		}
		imports.moduleAliases[alias] = "os"
	}
	for _, match := range pythonFromSubprocessPattern.FindAllStringSubmatch(source, -1) {
		for local, imported := range pythonDirectImports(match[pythonFromSubprocessPattern.SubexpIndex("names")]) {
			if _, ok := pythonSubprocessCalls[imported]; ok {
				imports.directCalls[local] = "subprocess." + imported
			}
		}
	}
	for _, match := range pythonFromOSPattern.FindAllStringSubmatch(source, -1) {
		for local, imported := range pythonDirectImports(match[pythonFromOSPattern.SubexpIndex("names")]) {
			if imported == "system" {
				imports.directCalls[local] = "os.system"
			}
		}
	}
	return imports
}

func pythonDirectImports(raw string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(raw, ",") {
		fields := strings.Fields(strings.TrimSpace(part))
		if len(fields) == 1 {
			out[fields[0]] = fields[0]
		}
		if len(fields) == 3 && fields[1] == "as" {
			out[fields[2]] = fields[0]
		}
	}
	return out
}

func pythonFunctionBodyEnd(lines []string, start int, indent int) int {
	for index := start; index < len(lines); index++ {
		line := lines[index]
		if strings.TrimSpace(line) == "" {
			continue
		}
		if len(line)-len(strings.TrimLeft(line, " \t")) <= indent {
			return index
		}
	}
	return len(lines)
}

func pythonIdentifierShadowedBefore(lines []string, identifier string) bool {
	pattern := regexp.MustCompile(`\b` + regexp.QuoteMeta(identifier) + `\s*=`)
	for _, line := range lines {
		if pattern.MatchString(line) {
			return true
		}
	}
	return false
}

func pythonLineCalls(line string, name string) bool {
	return regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\s*\(`).MatchString(line)
}
