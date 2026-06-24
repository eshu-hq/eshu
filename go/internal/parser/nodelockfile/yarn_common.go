// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package nodelockfile

import (
	"sort"
	"strings"
)

type yarnBlock struct {
	headerLine string
	bodyLines  []string
	lineNumber int
}

// walkYarnInstanceChains builds a chain map keyed by composite instance key
// ("name@version") from the universe of yarn instance keys and the
// per-instance dependency edges (already resolved to child instance keys).
// Instances with no incoming edges from any other instance are treated as
// importer-equivalent roots (yarn lockfiles do not carry a separate
// importer table the way pnpm does). The resulting chain is a slice of
// package names so callers can hand it to dependencyRow as the public
// `dependency_path` value.
func walkYarnInstanceChains(
	instanceKeys []string,
	adj map[string][]string,
	nameByInstance map[string]string,
) map[string][]string {
	hasIncoming := make(map[string]bool, len(instanceKeys))
	for _, children := range adj {
		for _, child := range children {
			hasIncoming[child] = true
		}
	}
	roots := make([]string, 0)
	seen := make(map[string]struct{}, len(instanceKeys))
	for _, key := range instanceKeys {
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if !hasIncoming[key] {
			roots = append(roots, key)
		}
	}
	sort.Strings(roots)
	chains := make(map[string][]string)
	for _, root := range roots {
		rootName := nameByInstance[root]
		if rootName == "" {
			continue
		}
		walkChainByInstance(root, []string{rootName}, adj, nameByInstance, chains)
	}
	for _, key := range instanceKeys {
		if _, ok := chains[key]; !ok {
			if name := nameByInstance[key]; name != "" {
				chains[key] = []string{name}
			}
		}
	}
	return chains
}

// yarnInstanceKey builds the composite key used to index yarn package
// instances. Using a name+version key prevents multiple installed versions
// of the same package from overwriting each other in adjacency maps and
// chain reconstruction.
func yarnInstanceKey(name, version string) string {
	return strings.TrimSpace(name) + "@" + strings.TrimSpace(version)
}

// walkChainByInstance walks an instance-keyed dependency graph and writes
// the resulting chain (as a slice of package names, in walk order) for each
// reachable instance into out. The adjacency map and out map are both keyed
// by composite instance keys (e.g. "lodash@4.17.21") so different versions
// of the same package do not collapse together; nameByInstance is the
// instance-key → display-name table used to translate the chain back to the
// public name-only form callers expect.
func walkChainByInstance(
	nodeKey string,
	chain []string,
	adj map[string][]string,
	nameByInstance map[string]string,
	out map[string][]string,
) {
	if existing, ok := out[nodeKey]; ok && len(existing) <= len(chain) {
		return
	}
	out[nodeKey] = append([]string(nil), chain...)
	for _, childKey := range adj[nodeKey] {
		childName := nameByInstance[childKey]
		if childName == "" {
			continue
		}
		if containsString(chain, childName) {
			continue
		}
		walkChainByInstance(childKey, append(append([]string(nil), chain...), childName), adj, nameByInstance, out)
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func startsWithWhitespace(line string) bool {
	if line == "" {
		return false
	}
	return line[0] == ' ' || line[0] == '\t'
}

// descriptorName extracts the package name from a yarn descriptor:
//
//	name@^1.0.0           -> name
//	@scope/name@^1.0.0    -> @scope/name
//	name@npm:^1.0.0       -> name (berry)
func descriptorName(descriptor string) string {
	descriptor = strings.TrimSpace(descriptor)
	if descriptor == "" {
		return ""
	}
	prefix := ""
	if strings.HasPrefix(descriptor, "@") {
		prefix = "@"
		descriptor = descriptor[1:]
	}
	index := strings.IndexByte(descriptor, '@')
	if index < 0 {
		return prefix + descriptor
	}
	return prefix + descriptor[:index]
}

func countLeadingSpaces(line string) int {
	count := 0
	for _, r := range line {
		if r == ' ' {
			count++
			continue
		}
		if r == '\t' {
			count += 2
			continue
		}
		break
	}
	return count
}

// splitOutsideQuotes splits a string on separator while ignoring instances
// inside double-quoted substrings.
func splitOutsideQuotes(source string, separator byte) []string {
	parts := make([]string, 0)
	var current strings.Builder
	inQuotes := false
	for i := 0; i < len(source); i++ {
		ch := source[i]
		if ch == '"' {
			inQuotes = !inQuotes
			current.WriteByte(ch)
			continue
		}
		if !inQuotes && ch == separator {
			parts = append(parts, current.String())
			current.Reset()
			continue
		}
		current.WriteByte(ch)
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}
	return parts
}

// isLocalProtocol returns true for protocols that point at on-disk code
// rather than a remote registry package. Those entries do not prove a
// remote package/version identity.
func isLocalProtocol(protocol string) bool {
	switch protocol {
	case "workspace", "file", "link", "portal":
		return true
	}
	return false
}

// isSupportedRemoteProtocol returns true for protocols Eshu's reducer can
// trust as a remote npm-ecosystem package version today.
func isSupportedRemoteProtocol(protocol string) bool {
	switch protocol {
	case "", "npm":
		return true
	}
	return false
}
