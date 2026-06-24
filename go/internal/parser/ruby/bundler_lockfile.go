// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ruby

import (
	"regexp"
	"slices"
	"strings"
)

var (
	bundlerLockSpecPattern       = regexp.MustCompile(`^    ([A-Za-z0-9_.-]+) \(([^)]+)\)\s*$`)
	bundlerLockDependencyPattern = regexp.MustCompile(`^      ([A-Za-z0-9_.-]+)(?: \(([^)]+)\))?\s*$`)
	bundlerLockDirectPattern     = regexp.MustCompile(`^  ([A-Za-z0-9_.-]+)!?(?: \(([^)]+)\))?\s*$`)
)

type bundlerLockSpec struct {
	name       string
	version    string
	line       int
	sourceType string
	sourcePath string
}

func parseBundlerLockfilePayload(path string, source []byte, isDependency bool) map[string]any {
	payload := newBundlerPayload(path, isDependency)
	specs, edges, directNames := parseBundlerLockfile(string(source))
	chains := bundlerLockDependencyChains(edges, directNames)

	names := make([]string, 0, len(specs))
	for name := range specs {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		spec := specs[name]
		chain := chains[name]
		row := bundlerDependencyRow{
			name:              spec.name,
			value:             spec.version,
			line:              spec.line,
			lockfile:          true,
			sourceType:        spec.sourceType,
			sourcePath:        spec.sourcePath,
			sourceAmbiguous:   spec.sourceType == bundlerSourceGit || spec.sourceType == bundlerSourcePath,
			dependencyPath:    chain,
			dependencySection: bundlerLockfileSection,
		}
		if len(chain) > 0 {
			row.dependencyDepth = len(chain)
			row.directDependency = bundlerBool(len(chain) == 1)
		}
		appendBundlerDependency(payload, row)
	}

	sharedSortBundlerVariables(payload)
	return payload
}

func parseBundlerLockfile(source string) (map[string]bundlerLockSpec, map[string][]string, []string) {
	specs := make(map[string]bundlerLockSpec)
	edges := make(map[string][]string)
	directNames := make([]string, 0)
	section := ""
	sourceType := ""
	sourcePath := ""
	inSpecs := false
	currentSpec := ""

	for index, rawLine := range strings.Split(source, "\n") {
		lineNumber := index + 1
		trimmed := strings.TrimSpace(rawLine)
		if trimmed == "" {
			continue
		}
		if isBundlerLockSectionHeader(rawLine, trimmed) {
			section = trimmed
			sourceType = bundlerLockSourceType(section)
			sourcePath = ""
			inSpecs = false
			currentSpec = ""
			continue
		}

		switch section {
		case "GEM", "GIT", "PATH":
			if strings.HasPrefix(trimmed, "remote:") {
				sourcePath = strings.TrimSpace(strings.TrimPrefix(trimmed, "remote:"))
				continue
			}
			if trimmed == "specs:" {
				inSpecs = true
				currentSpec = ""
				continue
			}
			if !inSpecs {
				continue
			}
			if matches := bundlerLockSpecPattern.FindStringSubmatch(rawLine); len(matches) == 3 {
				currentSpec = strings.TrimSpace(matches[1])
				specs[currentSpec] = bundlerLockSpec{
					name:       currentSpec,
					version:    strings.TrimSpace(matches[2]),
					line:       lineNumber,
					sourceType: sourceType,
					sourcePath: sourcePath,
				}
				continue
			}
			if matches := bundlerLockDependencyPattern.FindStringSubmatch(rawLine); len(matches) >= 2 && currentSpec != "" {
				dependencyName := strings.TrimSpace(matches[1])
				if dependencyName != "" {
					edges[currentSpec] = append(edges[currentSpec], dependencyName)
				}
			}
		case "DEPENDENCIES":
			if matches := bundlerLockDirectPattern.FindStringSubmatch(rawLine); len(matches) >= 2 {
				name := strings.TrimSpace(matches[1])
				if name != "" {
					directNames = append(directNames, name)
				}
			}
		}
	}

	return specs, edges, uniqueBundlerLockNames(directNames)
}

func isBundlerLockSectionHeader(rawLine string, trimmed string) bool {
	return strings.TrimRight(rawLine, "\r") == trimmed && strings.ToUpper(trimmed) == trimmed
}

func bundlerLockSourceType(section string) string {
	switch section {
	case "GIT":
		return bundlerSourceGit
	case "PATH":
		return bundlerSourcePath
	case "GEM":
		return bundlerSourceRubyGems
	default:
		return ""
	}
}

func bundlerLockDependencyChains(edges map[string][]string, directNames []string) map[string][]string {
	chains := make(map[string][]string)
	for _, direct := range directNames {
		walkBundlerLockDependency(chains, edges, direct, []string{direct}, map[string]struct{}{direct: {}})
	}
	return chains
}

func walkBundlerLockDependency(
	chains map[string][]string,
	edges map[string][]string,
	name string,
	path []string,
	seen map[string]struct{},
) {
	recordBundlerLockChain(chains, name, path)
	children := append([]string(nil), edges[name]...)
	slices.Sort(children)
	for _, child := range children {
		child = strings.TrimSpace(child)
		if child == "" {
			continue
		}
		if _, ok := seen[child]; ok {
			continue
		}
		nextSeen := make(map[string]struct{}, len(seen)+1)
		for existing := range seen {
			nextSeen[existing] = struct{}{}
		}
		nextSeen[child] = struct{}{}
		nextPath := append(append([]string(nil), path...), child)
		walkBundlerLockDependency(chains, edges, child, nextPath, nextSeen)
	}
}

func recordBundlerLockChain(chains map[string][]string, name string, path []string) {
	if name == "" || len(path) == 0 {
		return
	}
	existing := chains[name]
	if len(existing) == 0 || len(path) < len(existing) || sameLengthPathLess(path, existing) {
		chains[name] = append([]string(nil), path...)
	}
}

func sameLengthPathLess(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	return strings.Join(left, "\x00") < strings.Join(right, "\x00")
}

func uniqueBundlerLockNames(names []string) []string {
	seen := make(map[string]struct{}, len(names))
	out := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	slices.Sort(out)
	return out
}
