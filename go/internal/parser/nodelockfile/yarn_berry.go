// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
// what it understands. The dependency graph is keyed by composite
// "name@version" so two locators that pin the same package name to
// different versions (a common Berry pattern) do not silently overwrite
// each other.
func parseYarnBerryLockfile(source []byte, payload map[string]any) []map[string]any {
	blocks := splitYarnBerryBlocks(string(source))
	if len(blocks) == 0 {
		payload["lockfile_parse_state"] = "empty"
		return []map[string]any{}
	}

	type pkg struct {
		instanceKey string
		name        string
		version     string
		protocol    string
		unsupported string
		deps        map[string]string
		lineNumber  int
	}

	packages := make([]pkg, 0, len(blocks))
	// descriptorToKey maps every Berry locator string ("name@npm:^1.0.0")
	// to the instance key it resolves to. Berry headers can contain a
	// comma-separated locator list when multiple ranges share one
	// resolution; we index every locator so a parent block's
	// "dependencies: { name: range }" edge can be resolved back to the
	// specific (name, version) instance.
	descriptorToKey := make(map[string]string)
	nameByInstance := make(map[string]string)

	for _, block := range blocks {
		descriptors := parseYarnBerryDescriptorLine(block.headerLine)
		if len(descriptors) == 0 {
			continue
		}
		if len(descriptors) == 1 && descriptors[0] == "__metadata" {
			continue
		}
		// All descriptors in one block share the same name and resolution.
		name, protocol := splitBerryDescriptor(descriptors[0])
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
		// Workspace/file/link/portal entries do not prove a remote package
		// identity, so they must not be emitted as registry deps.
		if isLocalProtocol(protocol) {
			continue
		}
		unsupported := ""
		if !isSupportedRemoteProtocol(protocol) {
			unsupported = protocol
		}
		instanceKey := yarnInstanceKey(name, version)
		packages = append(packages, pkg{
			instanceKey: instanceKey,
			name:        name,
			version:     version,
			protocol:    protocol,
			unsupported: unsupported,
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

	// Build per-instance dependency adjacency by resolving each
	// "name@range" child to a Berry locator that matches a known instance.
	depAdjacency := make(map[string][]string, len(packages))
	for _, p := range packages {
		depAdjacency[p.instanceKey] = yarnBerryChildKeys(p.deps, descriptorToKey)
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
		row := dependencyRow(p.name, p.version, "yarn.lock", "yarn", "yarn-berry", index+1, chain)
		if p.protocol != "" {
			row["lockfile_resolution_protocol"] = p.protocol
		}
		if p.unsupported != "" {
			row["lockfile_unsupported_feature"] = p.unsupported
			row["config_kind"] = "unsupported_dependency"
		}
		rows = append(rows, row)
	}
	return rows
}

// yarnBerryChildKeys resolves a parent's "child name -> child range" deps to
// the matching instance keys by checking the known Berry protocols in
// priority order. Children whose descriptor matches no known locator are
// dropped so chain reconstruction does not invent edges to a phantom
// instance.
func yarnBerryChildKeys(deps map[string]string, descriptorToKey map[string]string) []string {
	if len(deps) == 0 {
		return nil
	}
	out := make([]string, 0, len(deps))
	seen := make(map[string]struct{}, len(deps))
	for _, name := range sortedKeysString(deps) {
		rng := deps[name]
		key := yarnBerryResolveChildKey(name, rng, descriptorToKey)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	return out
}

func yarnBerryResolveChildKey(name, rng string, descriptorToKey map[string]string) string {
	// Try common Berry protocols in priority order; npm:: is by far the
	// most common edge protocol but we also see virtual: and patch: in
	// real-world lockfiles.
	for _, protocol := range []string{"npm:", "patch:", "virtual:", ""} {
		descriptor := name + "@" + protocol + rng
		if key, ok := descriptorToKey[descriptor]; ok {
			return key
		}
	}
	return ""
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

// parseYarnBerryDescriptorLine splits a Berry header line into its locator
// strings. Berry locators are normally quoted and may appear in a
// comma-separated list when multiple ranges share one resolution
// (e.g. `"lodash@npm:^4.0.0, lodash@npm:^4.17.0":`).
func parseYarnBerryDescriptorLine(header string) []string {
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
