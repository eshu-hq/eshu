package json

import (
	"fmt"
	"sort"
	"strings"
)

func packageLockDependencyVariables(document map[string]any, lang string) []map[string]any {
	packages, ok := document["packages"].(map[string]any)
	if !ok {
		return packageLockV1DependencyVariables(document, lang)
	}
	chains := packageLockDependencyChains(packages)

	paths := make([]string, 0, len(packages))
	for path := range packages {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	rows := make([]map[string]any, 0, len(paths))
	for _, path := range paths {
		name := packageLockPackageName(path)
		if name == "" {
			continue
		}
		entry, ok := packages[path].(map[string]any)
		if !ok {
			continue
		}
		version := strings.TrimSpace(fmt.Sprint(entry["version"]))
		if version == "" || version == "<nil>" {
			continue
		}
		rows = append(rows, packageLockDependencyRow(name, version, len(rows)+1, lang, chains[path]))
	}
	return rows
}

func packageLockV1DependencyVariables(document map[string]any, lang string) []map[string]any {
	dependencies, ok := document["dependencies"].(map[string]any)
	if !ok {
		return nil
	}
	names := make([]string, 0, len(dependencies))
	for name := range dependencies {
		names = append(names, name)
	}
	sort.Strings(names)

	rows := make([]map[string]any, 0, len(names))
	for _, name := range names {
		entry, ok := dependencies[name].(map[string]any)
		if !ok {
			continue
		}
		version := strings.TrimSpace(fmt.Sprint(entry["version"]))
		if version == "" || version == "<nil>" {
			continue
		}
		rows = append(rows, packageLockDependencyRow(name, version, len(rows)+1, lang, nil))
	}
	return rows
}

func packageLockPackageName(path string) string {
	path = strings.Trim(strings.TrimSpace(path), "/")
	if path == "" {
		return ""
	}
	const marker = "node_modules/"
	index := strings.LastIndex(path, marker)
	if index < 0 {
		return ""
	}
	name := path[index+len(marker):]
	if strings.HasPrefix(name, "@") {
		parts := strings.Split(name, "/")
		if len(parts) < 2 {
			return ""
		}
		return parts[0] + "/" + parts[1]
	}
	name, _, _ = strings.Cut(name, "/")
	return strings.TrimSpace(name)
}

func packageLockDependencyRow(name string, version string, lineNumber int, lang string, dependencyPath []string) map[string]any {
	row := map[string]any{
		"name":            strings.TrimSpace(name),
		"line_number":     lineNumber,
		"value":           strings.TrimSpace(version),
		"section":         "package-lock",
		"config_kind":     "dependency",
		"package_manager": "npm",
		"lockfile":        true,
		"lang":            lang,
	}
	if len(dependencyPath) > 0 {
		row["dependency_path"] = dependencyPath
		row["dependency_depth"] = len(dependencyPath)
		row["direct_dependency"] = len(dependencyPath) == 1
	}
	return row
}

func packageLockDependencyChains(packages map[string]any) map[string][]string {
	pathsByName := packageLockPathsByName(packages)
	chains := map[string][]string{}
	rootDeps := packageLockEntryDependencies(packages[""])
	for _, name := range sortedMapKeys(rootDeps) {
		path := packageLockDependencyChildPath("", name, pathsByName)
		if path == "" {
			continue
		}
		packageLockWalkDependencyChains(path, []string{name}, packages, pathsByName, chains)
	}
	return chains
}

func packageLockWalkDependencyChains(
	path string,
	chain []string,
	packages map[string]any,
	pathsByName map[string][]string,
	chains map[string][]string,
) {
	if path == "" || len(chain) == 0 {
		return
	}
	if existing, ok := chains[path]; ok && len(existing) <= len(chain) {
		return
	}
	chains[path] = append([]string(nil), chain...)
	for _, name := range sortedMapKeys(packageLockEntryDependencies(packages[path])) {
		childPath := packageLockDependencyChildPath(path, name, pathsByName)
		if childPath == "" {
			continue
		}
		childChain := append(append([]string(nil), chain...), name)
		packageLockWalkDependencyChains(childPath, childChain, packages, pathsByName, chains)
	}
}

func packageLockPathsByName(packages map[string]any) map[string][]string {
	out := make(map[string][]string, len(packages))
	for path := range packages {
		name := packageLockPackageName(path)
		if name == "" {
			continue
		}
		out[name] = append(out[name], path)
	}
	for name := range out {
		sort.Strings(out[name])
	}
	return out
}

func packageLockDependencyChildPath(parentPath string, name string, pathsByName map[string][]string) string {
	paths := pathsByName[name]
	if len(paths) == 0 {
		return ""
	}
	if parentPath != "" {
		nestedPrefix := strings.TrimRight(parentPath, "/") + "/node_modules/"
		for _, path := range paths {
			if strings.HasPrefix(path, nestedPrefix) {
				return path
			}
		}
	}
	topLevelPath := "node_modules/" + name
	for _, path := range paths {
		if path == topLevelPath {
			return path
		}
	}
	return paths[0]
}

func packageLockEntryDependencies(raw any) map[string]any {
	entry, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]any)
	for _, key := range []string{"dependencies", "devDependencies", "optionalDependencies", "peerDependencies"} {
		deps, ok := entry[key].(map[string]any)
		if !ok {
			continue
		}
		for name, value := range deps {
			name = strings.TrimSpace(name)
			if name != "" {
				out[name] = value
			}
		}
	}
	return out
}
