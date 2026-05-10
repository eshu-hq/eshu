package rust

import (
	"bufio"
	"sort"
	"strings"
)

type cargoCfgManifest struct {
	PackageName           string
	WorkspaceMembers      []string
	FeatureNames          []string
	DefaultFeatureMembers []string
	TargetCfgSections     []cargoTargetCfgSection
}

type cargoTargetCfgSection struct {
	Expression     string
	DependencyKind string
}

// parseCargoCfgManifest scans only Cargo.toml signals needed by future Rust cfg resolution.
func parseCargoCfgManifest(text string) cargoCfgManifest {
	var manifest cargoCfgManifest
	featureNames := map[string]struct{}{}
	section := ""

	scanner := bufio.NewScanner(strings.NewReader(text))
	for scanner.Scan() {
		line := strings.TrimSpace(stripCargoComment(scanner.Text()))
		if line == "" {
			continue
		}
		if header, ok := parseCargoTableHeader(line); ok {
			section = normalizedCargoSection(header)
			if targetSection, ok := parseCargoTargetCfgSection(header); ok {
				manifest.TargetCfgSections = append(manifest.TargetCfgSections, targetSection)
			}
			continue
		}

		key, value, ok := splitCargoKeyValue(line)
		if !ok || strings.Contains(key, ".") {
			continue
		}

		switch section {
		case "package":
			if key == "name" {
				if name, ok := parseCargoString(value); ok {
					manifest.PackageName = name
				}
			}
		case "workspace":
			if key == "members" {
				if members, ok := parseCargoStringArray(value); ok {
					manifest.WorkspaceMembers = members
				}
			}
		case "features":
			members, ok := parseCargoStringArray(value)
			if !ok {
				continue
			}
			featureNames[key] = struct{}{}
			if key == "default" {
				manifest.DefaultFeatureMembers = members
			}
		}
	}

	manifest.FeatureNames = sortedCargoKeys(featureNames)
	return manifest
}

func stripCargoComment(line string) string {
	inSingle := false
	inDouble := false
	escaped := false
	for idx, r := range line {
		switch {
		case escaped:
			escaped = false
		case r == '\\' && inDouble:
			escaped = true
		case r == '\'' && !inDouble:
			inSingle = !inSingle
		case r == '"' && !inSingle:
			inDouble = !inDouble
		case r == '#' && !inSingle && !inDouble:
			return line[:idx]
		}
	}
	return line
}

func parseCargoTableHeader(line string) (string, bool) {
	if !strings.HasPrefix(line, "[") || !strings.HasSuffix(line, "]") {
		return "", false
	}
	header := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]"))
	if header == "" || strings.HasPrefix(header, "[") || strings.HasSuffix(header, "]") {
		return "", false
	}
	return header, true
}

func normalizedCargoSection(header string) string {
	parts := splitCargoHeaderParts(header)
	if len(parts) == 0 {
		return ""
	}
	if len(parts) == 1 {
		return strings.TrimSpace(parts[0])
	}
	return ""
}

func parseCargoTargetCfgSection(header string) (cargoTargetCfgSection, bool) {
	parts := splitCargoHeaderParts(header)
	if len(parts) < 3 || strings.TrimSpace(parts[0]) != "target" {
		return cargoTargetCfgSection{}, false
	}
	expression := unquoteCargoHeaderPart(strings.TrimSpace(parts[1]))
	if !strings.HasPrefix(expression, "cfg(") || !strings.HasSuffix(expression, ")") {
		return cargoTargetCfgSection{}, false
	}
	dependencyKind := strings.TrimSpace(parts[2])
	switch dependencyKind {
	case "dependencies", "dev-dependencies", "build-dependencies":
		return cargoTargetCfgSection{Expression: expression, DependencyKind: dependencyKind}, true
	default:
		return cargoTargetCfgSection{}, false
	}
}

func splitCargoHeaderParts(header string) []string {
	var parts []string
	start := 0
	inSingle := false
	inDouble := false
	escaped := false
	for idx, r := range header {
		switch {
		case escaped:
			escaped = false
		case r == '\\' && inDouble:
			escaped = true
		case r == '\'' && !inDouble:
			inSingle = !inSingle
		case r == '"' && !inSingle:
			inDouble = !inDouble
		case r == '.' && !inSingle && !inDouble:
			parts = append(parts, strings.TrimSpace(header[start:idx]))
			start = idx + 1
		}
	}
	parts = append(parts, strings.TrimSpace(header[start:]))
	return parts
}

func splitCargoKeyValue(line string) (string, string, bool) {
	inSingle := false
	inDouble := false
	escaped := false
	for idx, r := range line {
		switch {
		case escaped:
			escaped = false
		case r == '\\' && inDouble:
			escaped = true
		case r == '\'' && !inDouble:
			inSingle = !inSingle
		case r == '"' && !inSingle:
			inDouble = !inDouble
		case r == '=' && !inSingle && !inDouble:
			key := unquoteCargoHeaderPart(strings.TrimSpace(line[:idx]))
			value := strings.TrimSpace(line[idx+1:])
			return key, value, key != "" && value != ""
		}
	}
	return "", "", false
}

func parseCargoString(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if len(value) < 2 || value[0] != '"' || value[len(value)-1] != '"' {
		return "", false
	}
	return unescapeCargoDoubleQuoted(value[1 : len(value)-1])
}

func parseCargoStringArray(value string) ([]string, bool) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "[") || !strings.HasSuffix(value, "]") {
		return nil, false
	}
	body := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(value, "["), "]"))
	if body == "" {
		return []string{}, true
	}

	var values []string
	parts := splitCargoArrayItems(body)
	for idx, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" && idx == len(parts)-1 {
			continue
		}
		item, ok := parseCargoString(part)
		if !ok {
			return nil, false
		}
		values = append(values, item)
	}
	return values, true
}

func splitCargoArrayItems(body string) []string {
	var parts []string
	start := 0
	inDouble := false
	escaped := false
	for idx, r := range body {
		switch {
		case escaped:
			escaped = false
		case r == '\\' && inDouble:
			escaped = true
		case r == '"':
			inDouble = !inDouble
		case r == ',' && !inDouble:
			parts = append(parts, body[start:idx])
			start = idx + 1
		}
	}
	parts = append(parts, body[start:])
	return parts
}

func unquoteCargoHeaderPart(part string) string {
	if len(part) < 2 {
		return part
	}
	if (part[0] == '\'' && part[len(part)-1] == '\'') || (part[0] == '"' && part[len(part)-1] == '"') {
		return part[1 : len(part)-1]
	}
	return part
}

func unescapeCargoDoubleQuoted(value string) (string, bool) {
	var builder strings.Builder
	escaped := false
	for _, r := range value {
		if escaped {
			switch r {
			case '"', '\\':
				builder.WriteRune(r)
			default:
				return "", false
			}
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		builder.WriteRune(r)
	}
	if escaped {
		return "", false
	}
	return builder.String(), true
}

func sortedCargoKeys(values map[string]struct{}) []string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
