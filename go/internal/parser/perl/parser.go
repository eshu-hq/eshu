package perl

import (
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

var (
	perlPackagePattern  = regexp.MustCompile(`^\s*package\s+([A-Za-z_]\w*(?:::[A-Za-z_]\w*)*)\s*;`)
	perlUsePattern      = regexp.MustCompile(`^\s*use\s+([A-Za-z_]\w*(?:::[A-Za-z_]\w*)*)`)
	perlSubPattern      = regexp.MustCompile(`^\s*sub\s+([A-Za-z_]\w*)`)
	perlSpecialPattern  = regexp.MustCompile(`^\s*(BEGIN|UNITCHECK|CHECK|INIT|END)\s*\{`)
	perlExportPattern   = regexp.MustCompile(`^\s*(?:our\s+)?@(?:EXPORT|EXPORT_OK)\s*=\s*qw\((.*)`)
	perlVariablePattern = regexp.MustCompile(`\b(?:my|our)\s+[@$%]?([A-Za-z_]\w*)`)
	perlCallPattern     = regexp.MustCompile(`([A-Za-z_:]+::[A-Za-z_]\w*|[A-Za-z_]\w*)\s*\(`)
)

// Parse reads path and returns the legacy Perl parser payload.
func Parse(path string, isDependency bool, options shared.Options) (map[string]any, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, err
	}

	payload := shared.BasePayload(path, "perl", isDependency)
	lines := strings.Split(string(source), "\n")
	seenVariables := make(map[string]struct{})
	seenCalls := make(map[string]struct{})
	exportsByPackage := make(map[string]map[string]struct{})
	functionItems := make(map[string]map[string]any)
	currentPackage := ""
	exportPackage := ""
	collectingExports := false

	for index, rawLine := range lines {
		lineNumber := index + 1
		trimmed := strings.TrimSpace(rawLine)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if collectingExports {
			exportedSubs := perlExportsForPackage(exportsByPackage, exportPackage)
			done := perlCollectExportNames(trimmed, exportedSubs)
			if done {
				collectingExports = false
				perlRefreshExportedFunctionRoots(functionItems, exportPackage, exportedSubs)
			}
			continue
		}
		if matches := perlExportPattern.FindStringSubmatch(trimmed); len(matches) == 2 {
			exportPackage = currentPackage
			exportedSubs := perlExportsForPackage(exportsByPackage, exportPackage)
			if !perlCollectExportNames(matches[1], exportedSubs) {
				collectingExports = true
			}
			perlRefreshExportedFunctionRoots(functionItems, exportPackage, exportedSubs)
			continue
		}
		if matches := perlPackagePattern.FindStringSubmatch(trimmed); len(matches) == 2 {
			currentPackage = matches[1]
			item := map[string]any{
				"name":        shared.LastPathSegment(matches[1], "::"),
				"line_number": lineNumber,
				"end_line":    lineNumber,
				"lang":        "perl",
			}
			if perlIsPublicPackage(currentPackage) {
				item["dead_code_root_kinds"] = []string{"perl.package_namespace"}
			}
			shared.AppendBucket(payload, "classes", item)
		}
		if matches := perlUsePattern.FindStringSubmatch(trimmed); len(matches) == 2 {
			shared.AppendBucket(payload, "imports", map[string]any{
				"name":        matches[1],
				"line_number": lineNumber,
				"lang":        "perl",
			})
		}
		if matches := perlSpecialPattern.FindStringSubmatch(trimmed); len(matches) == 2 {
			item := map[string]any{
				"name":                 matches[1],
				"line_number":          lineNumber,
				"end_line":             lineNumber,
				"lang":                 "perl",
				"decorators":           []string{},
				"dead_code_root_kinds": []string{"perl.special_block"},
			}
			if currentPackage != "" {
				item["class_context"] = shared.LastPathSegment(currentPackage, "::")
			}
			if options.IndexSource {
				item["source"] = rawLine
			}
			shared.AppendBucket(payload, "functions", item)
		}
		if matches := perlSubPattern.FindStringSubmatch(trimmed); len(matches) == 2 {
			name := matches[1]
			item := map[string]any{
				"name":        name,
				"line_number": lineNumber,
				"end_line":    lineNumber,
				"lang":        "perl",
				"decorators":  []string{},
			}
			if currentPackage != "" {
				item["class_context"] = shared.LastPathSegment(currentPackage, "::")
			}
			exportedSubs := exportsByPackage[currentPackage]
			addPerlRootKind(item, perlFunctionRootKinds(name, currentPackage, exportedSubs, path)...)
			if options.IndexSource {
				item["source"] = rawLine
			}
			functionItems[perlFunctionKey(currentPackage, name)] = item
			shared.AppendBucket(payload, "functions", item)
		}
		for _, match := range perlVariablePattern.FindAllStringSubmatch(trimmed, -1) {
			if len(match) != 2 {
				continue
			}
			name := match[1]
			if _, ok := seenVariables[name]; ok {
				continue
			}
			seenVariables[name] = struct{}{}
			shared.AppendBucket(payload, "variables", map[string]any{
				"name":        name,
				"line_number": lineNumber,
				"end_line":    lineNumber,
				"lang":        "perl",
			})
		}
		for _, match := range perlCallPattern.FindAllStringSubmatch(trimmed, -1) {
			if len(match) != 2 {
				continue
			}
			appendUniqueRegexCall(payload, seenCalls, match[1], lineNumber, "perl")
		}
	}

	shared.SortNamedBucket(payload, "functions")
	shared.SortNamedBucket(payload, "classes")
	shared.SortNamedBucket(payload, "variables")
	shared.SortNamedBucket(payload, "imports")
	shared.SortNamedBucket(payload, "function_calls")
	return payload, nil
}

func perlExportsForPackage(exportsByPackage map[string]map[string]struct{}, packageName string) map[string]struct{} {
	if exportsByPackage[packageName] == nil {
		exportsByPackage[packageName] = make(map[string]struct{})
	}
	return exportsByPackage[packageName]
}

func perlCollectExportNames(line string, exports map[string]struct{}) bool {
	segment, _, done := strings.Cut(line, ")")
	for _, field := range strings.Fields(segment) {
		name := strings.Trim(field, " \t\r\n,;")
		name = strings.TrimLeft(name, "$@%&")
		if name == "" || !isPerlIdentifier(name) {
			continue
		}
		exports[name] = struct{}{}
	}
	return done
}

func perlRefreshExportedFunctionRoots(functionItems map[string]map[string]any, packageName string, exportedSubs map[string]struct{}) {
	for name := range exportedSubs {
		item := functionItems[perlFunctionKey(packageName, name)]
		if item == nil {
			continue
		}
		addPerlRootKind(item, "perl.exported_subroutine")
	}
}

func perlFunctionKey(packageName string, name string) string {
	if packageName == "" {
		return name
	}
	return packageName + "::" + name
}

func perlFunctionRootKinds(name string, currentPackage string, exportedSubs map[string]struct{}, path string) []string {
	var kinds []string
	if name == "main" && isPerlScriptPath(path) {
		kinds = append(kinds, "perl.script_entrypoint")
	}
	if exportedSubs != nil {
		if _, ok := exportedSubs[name]; ok {
			kinds = append(kinds, "perl.exported_subroutine")
		}
	}
	if currentPackage != "" && name == "new" {
		kinds = append(kinds, "perl.constructor")
	}
	switch name {
	case "AUTOLOAD":
		kinds = append(kinds, "perl.autoload_subroutine")
	case "DESTROY":
		kinds = append(kinds, "perl.destroy_subroutine")
	}
	return kinds
}

func addPerlRootKind(item map[string]any, kinds ...string) {
	if len(kinds) == 0 {
		return
	}
	existing, _ := item["dead_code_root_kinds"].([]string)
	for _, kind := range kinds {
		if kind == "" || slices.Contains(existing, kind) {
			continue
		}
		existing = append(existing, kind)
	}
	if len(existing) > 0 {
		slices.Sort(existing)
		item["dead_code_root_kinds"] = existing
	}
}

func isPerlScriptPath(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".pl" || ext == ".t"
}

func perlIsPublicPackage(name string) bool {
	for _, part := range strings.Split(name, "::") {
		if part == "" || strings.HasPrefix(part, "_") {
			return false
		}
	}
	return true
}

func isPerlIdentifier(name string) bool {
	if name == "" {
		return false
	}
	for index, char := range name {
		if index == 0 {
			if (char < 'A' || char > 'Z') && (char < 'a' || char > 'z') && char != '_' {
				return false
			}
			continue
		}
		if (char < 'A' || char > 'Z') && (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '_' {
			return false
		}
	}
	return true
}

// PreScan returns Perl subroutine and package names used by repository pre-scan.
func PreScan(path string) ([]string, error) {
	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		return nil, err
	}
	names := shared.CollectBucketNames(payload, "functions", "classes")
	slices.Sort(names)
	return names, nil
}

func appendUniqueRegexCall(
	payload map[string]any,
	seen map[string]struct{},
	fullName string,
	lineNumber int,
	lang string,
) {
	if strings.TrimSpace(fullName) == "" {
		return
	}
	if _, ok := seen[fullName]; ok {
		return
	}
	seen[fullName] = struct{}{}
	shared.AppendBucket(payload, "function_calls", map[string]any{
		"name":        fullName,
		"full_name":   fullName,
		"line_number": lineNumber,
		"lang":        lang,
	})
}
