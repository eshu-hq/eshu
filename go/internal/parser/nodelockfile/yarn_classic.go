package nodelockfile

import (
	"sort"
	"strings"
)

// parseYarnClassicLockfile parses Yarn 1.x lockfiles. The format is a custom
// (non-YAML) shape: top-level descriptor blocks like
//
//	"name@^1.0.0", name@^1.0.1:
//	  version "1.0.5"
//	  resolved "..."
//	  dependencies:
//	    other "^2.0.0"
//
// Top-level blocks start at column 0; property lines are indented two spaces.
// We collect all resolved packages, then walk the importer-side dependencies
// graph from the root descriptors to produce dependency_path chains.
func parseYarnClassicLockfile(source []byte, payload map[string]any) []map[string]any {
	blocks := splitYarnClassicBlocks(string(source))
	if len(blocks) == 0 {
		payload["lockfile_parse_state"] = "empty"
		return []map[string]any{}
	}

	type pkg struct {
		name       string
		version    string
		lineNumber int
	}

	packages := make([]pkg, 0, len(blocks))
	depsByName := make(map[string]map[string]string)

	for _, block := range blocks {
		descriptors := parseYarnClassicDescriptorLine(block.headerLine)
		if len(descriptors) == 0 {
			continue
		}
		name := descriptorName(descriptors[0])
		if name == "" {
			continue
		}
		version, deps := parseYarnClassicProperties(block.bodyLines)
		if version == "" {
			continue
		}
		packages = append(packages, pkg{name: name, version: version, lineNumber: block.lineNumber})
		depsByName[name] = deps
	}

	if len(packages) == 0 {
		payload["lockfile_parse_state"] = "empty"
		return []map[string]any{}
	}

	names := make([]string, 0, len(packages))
	for _, p := range packages {
		names = append(names, p.name)
	}
	chains := walkLockfileDependencyChains(names, depsByName)

	sort.Slice(packages, func(i, j int) bool {
		return packages[i].name < packages[j].name
	})

	rows := make([]map[string]any, 0, len(packages))
	for index, p := range packages {
		row := dependencyRow(p.name, p.version, "yarn.lock", "yarn", "yarn-classic", index+1, chains[p.name])
		rows = append(rows, row)
	}
	return rows
}

// splitYarnClassicBlocks splits a yarn classic lockfile into header/body
// blocks. A header starts at column 0 (no leading whitespace), is not a
// comment, and ends with a colon; body lines follow with at least two spaces
// of indentation until the next header or EOF.
func splitYarnClassicBlocks(source string) []yarnBlock {
	lines := strings.Split(source, "\n")
	blocks := make([]yarnBlock, 0)
	var current *yarnBlock
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			if current != nil {
				// blank lines inside body terminate the block
				blocks = append(blocks, *current)
				current = nil
			}
			continue
		}
		if !startsWithWhitespace(line) {
			if current != nil {
				blocks = append(blocks, *current)
			}
			if !strings.HasSuffix(trimmed, ":") {
				current = nil
				continue
			}
			current = &yarnBlock{headerLine: trimmed, lineNumber: index + 1}
			continue
		}
		if current != nil {
			current.bodyLines = append(current.bodyLines, line)
		}
	}
	if current != nil {
		blocks = append(blocks, *current)
	}
	return blocks
}

// parseYarnClassicDescriptorLine splits a classic header line of the form
// `"name@^1.0.0", name@^1.0.1:` into individual descriptor strings.
func parseYarnClassicDescriptorLine(header string) []string {
	header = strings.TrimSuffix(strings.TrimSpace(header), ":")
	if header == "" {
		return nil
	}
	parts := splitOutsideQuotes(header, ',')
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		descriptor := strings.Trim(strings.TrimSpace(part), `"`)
		if descriptor != "" {
			out = append(out, descriptor)
		}
	}
	return out
}

func parseYarnClassicProperties(body []string) (string, map[string]string) {
	version := ""
	deps := make(map[string]string)
	inDependencies := false
	for _, line := range body {
		raw := line
		indent := countLeadingSpaces(raw)
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		if indent <= 2 {
			inDependencies = false
		}
		if indent == 2 && strings.HasPrefix(trimmed, "version ") {
			version = strings.Trim(strings.TrimPrefix(trimmed, "version "), `"`)
			continue
		}
		if indent == 2 && trimmed == "dependencies:" {
			inDependencies = true
			continue
		}
		if inDependencies && indent >= 4 {
			name, value := parseClassicDepLine(trimmed)
			if name != "" {
				deps[name] = value
			}
		}
	}
	return version, deps
}

func parseClassicDepLine(line string) (string, string) {
	parts := strings.SplitN(line, " ", 2)
	if len(parts) == 0 {
		return "", ""
	}
	name := strings.Trim(parts[0], `"`)
	value := ""
	if len(parts) == 2 {
		value = strings.Trim(strings.TrimSpace(parts[1]), `"`)
	}
	return name, value
}
