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

// walkLockfileDependencyChains builds a chain map keyed by package name from
// the universe of names and the per-package dependency edges. Packages whose
// name does not appear as a dependency of any other package are treated as
// importer-equivalent roots (yarn lockfiles do not carry a separate importer
// table the way pnpm does).
func walkLockfileDependencyChains(names []string, depsByName map[string]map[string]string) map[string][]string {
	dependencyAdj := make(map[string][]string, len(depsByName))
	for name, deps := range depsByName {
		dependencyAdj[name] = append(dependencyAdj[name], sortedKeys(deps)...)
	}
	hasIncoming := make(map[string]bool, len(names))
	for _, deps := range dependencyAdj {
		for _, dep := range deps {
			hasIncoming[dep] = true
		}
	}
	roots := make([]string, 0)
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		if !hasIncoming[name] {
			roots = append(roots, name)
		}
	}
	sort.Strings(roots)
	chains := make(map[string][]string)
	for _, root := range roots {
		walkChain(root, []string{root}, dependencyAdj, chains)
	}
	for _, name := range names {
		if _, ok := chains[name]; !ok {
			chains[name] = []string{name}
		}
	}
	return chains
}

// walkChain performs a bounded BFS-by-name to record the shortest dependency
// path from a root to every reachable node. Cycles are detected by chain
// membership; we never grow a chain past a node already in it.
func walkChain(node string, chain []string, adj map[string][]string, out map[string][]string) {
	if existing, ok := out[node]; ok && len(existing) <= len(chain) {
		return
	}
	out[node] = append([]string(nil), chain...)
	for _, child := range adj[node] {
		if containsString(chain, child) {
			continue
		}
		walkChain(child, append(append([]string(nil), chain...), child), adj, out)
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

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
