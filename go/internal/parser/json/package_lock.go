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
		rows = append(rows, packageLockDependencyRow(name, version, len(rows)+1, lang))
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
		rows = append(rows, packageLockDependencyRow(name, version, len(rows)+1, lang))
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

func packageLockDependencyRow(name string, version string, lineNumber int, lang string) map[string]any {
	return map[string]any{
		"name":            strings.TrimSpace(name),
		"line_number":     lineNumber,
		"value":           strings.TrimSpace(version),
		"section":         "package-lock",
		"config_kind":     "dependency",
		"package_manager": "npm",
		"lockfile":        true,
		"lang":            lang,
	}
}
