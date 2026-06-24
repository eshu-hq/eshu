// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
// graph from the root descriptors to produce dependency_path chains. The
// dependency graph is keyed by composite "name@version" so multiple installed
// versions of the same package name stay independent (yarn lockfiles often
// pin different ranges of the same dependency to different versions).
func parseYarnClassicLockfile(source []byte, payload map[string]any) []map[string]any {
	blocks := splitYarnClassicBlocks(string(source))
	if len(blocks) == 0 {
		payload["lockfile_parse_state"] = "empty"
		return []map[string]any{}
	}

	type pkg struct {
		instanceKey string
		name        string
		version     string
		deps        map[string]string
		lineNumber  int
	}

	packages := make([]pkg, 0, len(blocks))
	// descriptorToKey maps every header descriptor ("name@^1.0.0") to the
	// instance key its block resolved to. The same instance commonly answers
	// to several descriptors (e.g. "lodash@^4.0.0", "lodash@^4.17.0"). We
	// use this index to resolve a parent block's dependency references back
	// to a specific (name, version) instance.
	descriptorToKey := make(map[string]string)
	nameByInstance := make(map[string]string)

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
		instanceKey := yarnInstanceKey(name, version)
		packages = append(packages, pkg{
			instanceKey: instanceKey,
			name:        name,
			version:     version,
			deps:        deps,
			lineNumber:  block.lineNumber,
		})
		nameByInstance[instanceKey] = name
		for _, descriptor := range descriptors {
			descriptorToKey[descriptor] = instanceKey
		}
	}

	if len(packages) == 0 {
		payload["lockfile_parse_state"] = "empty"
		return []map[string]any{}
	}

	// Build per-instance dependency adjacency by resolving each "name@range"
	// child descriptor to an instance key.
	depAdjacency := make(map[string][]string, len(packages))
	for _, p := range packages {
		depAdjacency[p.instanceKey] = yarnClassicChildKeys(p.deps, descriptorToKey)
	}

	instanceKeys := make([]string, 0, len(packages))
	for _, p := range packages {
		instanceKeys = append(instanceKeys, p.instanceKey)
	}
	chains := walkYarnInstanceChains(instanceKeys, depAdjacency, nameByInstance)

	sort.Slice(packages, func(i, j int) bool {
		if packages[i].name != packages[j].name {
			return packages[i].name < packages[j].name
		}
		return packages[i].version < packages[j].version
	})

	rows := make([]map[string]any, 0, len(packages))
	for index, p := range packages {
		chain := chains[p.instanceKey]
		row := dependencyRow(p.name, p.version, "yarn.lock", "yarn", "yarn-classic", index+1, chain)
		rows = append(rows, row)
	}
	return rows
}

// yarnClassicChildKeys resolves a parent's "child name -> child range" deps
// to the matching instance keys. If a child descriptor does not map to a
// known instance, it is dropped so chain reconstruction does not invent
// edges.
func yarnClassicChildKeys(deps map[string]string, descriptorToKey map[string]string) []string {
	if len(deps) == 0 {
		return nil
	}
	out := make([]string, 0, len(deps))
	for _, name := range sortedKeysString(deps) {
		rng := deps[name]
		descriptor := name + "@" + rng
		key, ok := descriptorToKey[descriptor]
		if !ok {
			continue
		}
		out = append(out, key)
	}
	return out
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
