package nodelockfile

import (
	"sort"
	"strings"
)

// parseYarnBerryLockfile parses Yarn Berry (v2+) lockfiles. The file is
// YAML-compatible: top-level entries map descriptor keys to a small object
// with `version`, `resolution`, `dependencies`, etc. The descriptor encodes
// the resolution protocol (npm:, workspace:, patch:, file:, link:, exec:,
// portal:), which we surface so vulnerability impact stays honest about
// what it understands.
func parseYarnBerryLockfile(source []byte, payload map[string]any) []map[string]any {
	blocks := splitYarnBerryBlocks(string(source))
	if len(blocks) == 0 {
		payload["lockfile_parse_state"] = "empty"
		return []map[string]any{}
	}

	type pkg struct {
		name        string
		version     string
		protocol    string
		unsupported string
		lineNumber  int
	}

	packages := make([]pkg, 0, len(blocks))
	depsByName := make(map[string]map[string]string)

	for _, block := range blocks {
		descriptor := strings.Trim(strings.TrimSuffix(strings.TrimSpace(block.headerLine), ":"), `"`)
		if descriptor == "" || descriptor == "__metadata" {
			continue
		}
		name, protocol := splitBerryDescriptor(descriptor)
		if name == "" {
			continue
		}
		version, resolution, deps := parseYarnBerryProperties(block.bodyLines)
		if version == "" {
			continue
		}
		if protocol == "" {
			protocol = resolutionProtocol(resolution)
		}
		// Workspace/file/link/portal/exec entries do not prove a remote
		// package identity, so they must not be emitted as registry deps.
		if isLocalProtocol(protocol) {
			continue
		}
		unsupported := ""
		if !isSupportedRemoteProtocol(protocol) {
			unsupported = protocol
		}
		packages = append(packages, pkg{
			name:        name,
			version:     version,
			protocol:    protocol,
			unsupported: unsupported,
			lineNumber:  block.lineNumber,
		})
		if _, exists := depsByName[name]; !exists {
			depsByName[name] = deps
		}
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
		row := dependencyRow(p.name, p.version, "yarn.lock", "yarn", "yarn-berry", index+1, chains[p.name])
		if p.protocol != "" {
			row["lockfile_resolution_protocol"] = p.protocol
		}
		if p.unsupported != "" {
			row["lockfile_unsupported_feature"] = p.unsupported
		}
		rows = append(rows, row)
	}
	return rows
}

// splitYarnBerryBlocks splits a yarn berry lockfile into header/body blocks.
// Berry headers are descriptor strings followed by a colon, often quoted.
func splitYarnBerryBlocks(source string) []yarnBlock {
	lines := strings.Split(source, "\n")
	blocks := make([]yarnBlock, 0)
	var current *yarnBlock
	for index, line := range lines {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if !startsWithWhitespace(line) {
			if current != nil {
				blocks = append(blocks, *current)
			}
			if !strings.HasSuffix(strings.TrimSpace(line), ":") {
				current = nil
				continue
			}
			current = &yarnBlock{headerLine: strings.TrimSpace(line), lineNumber: index + 1}
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

// splitBerryDescriptor returns the package name and resolution protocol parsed
// from a yarn berry descriptor like `name@npm:^1.0.0` or
// `name@workspace:./packages/inner`.
func splitBerryDescriptor(descriptor string) (string, string) {
	name := descriptorName(descriptor)
	if name == "" {
		return "", ""
	}
	rest := strings.TrimPrefix(strings.TrimPrefix(descriptor, name), "@")
	if rest == "" {
		return name, ""
	}
	if colon := strings.IndexByte(rest, ':'); colon >= 0 {
		return name, strings.ToLower(strings.TrimSpace(rest[:colon]))
	}
	return name, ""
}

func parseYarnBerryProperties(body []string) (string, string, map[string]string) {
	version := ""
	resolution := ""
	deps := make(map[string]string)
	inDependencies := false
	for _, line := range body {
		indent := countLeadingSpaces(line)
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if indent <= 2 {
			inDependencies = false
		}
		if indent == 2 && strings.HasPrefix(trimmed, "version:") {
			version = strings.TrimSpace(strings.TrimPrefix(trimmed, "version:"))
			version = strings.Trim(version, `"`)
			continue
		}
		if indent == 2 && strings.HasPrefix(trimmed, "resolution:") {
			resolution = strings.TrimSpace(strings.TrimPrefix(trimmed, "resolution:"))
			resolution = strings.Trim(resolution, `"`)
			continue
		}
		if indent == 2 && trimmed == "dependencies:" {
			inDependencies = true
			continue
		}
		if inDependencies && indent >= 4 {
			name, value := parseBerryDepLine(trimmed)
			if name != "" {
				deps[name] = value
			}
		}
	}
	return version, resolution, deps
}

func parseBerryDepLine(line string) (string, string) {
	if idx := strings.Index(line, ":"); idx >= 0 {
		name := strings.Trim(strings.TrimSpace(line[:idx]), `"`)
		value := strings.Trim(strings.TrimSpace(line[idx+1:]), `"`)
		return name, value
	}
	return "", ""
}

// resolutionProtocol extracts the protocol from a berry resolution string
// like `"lodash@npm:4.17.21"` or `"patched-lib@patch:patched-lib@npm:1.0.0#./fix.diff"`.
func resolutionProtocol(resolution string) string {
	resolution = strings.TrimSpace(resolution)
	if resolution == "" {
		return ""
	}
	working := strings.TrimPrefix(resolution, "@")
	atIndex := strings.IndexByte(working, '@')
	if atIndex < 0 {
		return ""
	}
	rest := working[atIndex+1:]
	if colon := strings.IndexByte(rest, ':'); colon >= 0 {
		return strings.ToLower(strings.TrimSpace(rest[:colon]))
	}
	return ""
}
