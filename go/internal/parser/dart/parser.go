package dart

import (
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

var (
	dartImportPattern    = regexp.MustCompile(`^\s*(?:import|export)\s+'([^']+)'`)
	dartClassPattern     = regexp.MustCompile(`^\s*(?:abstract\s+)?class\s+([A-Za-z_]\w*)(?:<[^>{}]+>)?(?:\s+extends\s+([A-Za-z_]\w*(?:<[^>{}]+>)?))?`)
	dartMixinPattern     = regexp.MustCompile(`^\s*mixin\s+([A-Za-z_]\w*)`)
	dartEnumPattern      = regexp.MustCompile(`^\s*enum\s+([A-Za-z_]\w*)`)
	dartExtensionPattern = regexp.MustCompile(`^\s*extension\s+([A-Za-z_]\w*)\s+on\b`)
	dartFunctionPattern  = regexp.MustCompile(`^\s*(?:static\s+)?(?:[\w<>\?\[\], ]+\s+)?([A-Za-z_]\w*)\s*\(([^)]*)\)\s*(?:async\*?|async|=>|\{)`)
	dartVariablePattern  = regexp.MustCompile(`^\s*(?:final|var|const)\s+(?:[\w<>\?\[\], ]+\s+)?([A-Za-z_]\w*)\s*=`)
	dartCallPattern      = regexp.MustCompile(`\b([A-Za-z_]\w*)\s*\(`)
)

type classScope struct {
	name               string
	extends            string
	constructorPattern *regexp.Regexp
	braceDepth         int
}

// Parse reads path and returns the legacy Dart parser payload.
func Parse(path string, isDependency bool, options shared.Options) (map[string]any, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, err
	}

	payload := shared.BasePayload(path, "dart", isDependency)
	lines := strings.Split(string(source), "\n")
	seenVariables := make(map[string]struct{})
	seenCalls := make(map[string]struct{})
	var currentClass *classScope
	var pendingAnnotations []string
	publicLibraryPath := isPublicLibraryPath(path)

	for index, rawLine := range lines {
		lineNumber := index + 1
		trimmed := strings.TrimSpace(rawLine)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}
		if strings.HasPrefix(trimmed, "@") {
			pendingAnnotations = append(pendingAnnotations, strings.Fields(trimmed)[0])
			continue
		}

		if matches := dartImportPattern.FindStringSubmatch(trimmed); len(matches) == 2 {
			shared.AppendBucket(payload, "imports", map[string]any{
				"name":        matches[1],
				"line_number": lineNumber,
				"lang":        "dart",
			})
		}
		if matches := dartClassPattern.FindStringSubmatch(trimmed); len(matches) >= 2 {
			name := matches[1]
			extends := ""
			if len(matches) > 2 {
				extends = matches[2]
			}
			item := map[string]any{
				"name":        name,
				"line_number": lineNumber,
				"end_line":    lineNumber,
				"lang":        "dart",
			}
			addDartRootKind(item, dartClassRootKinds(name, publicLibraryPath)...)
			shared.AppendBucket(payload, "classes", item)
			currentClass = newDartClassScope(name, extends)
			pendingAnnotations = nil
		} else {
			for _, pattern := range []*regexp.Regexp{
				dartMixinPattern, dartEnumPattern, dartExtensionPattern,
			} {
				if matches := pattern.FindStringSubmatch(trimmed); len(matches) == 2 {
					item := map[string]any{
						"name":        matches[1],
						"line_number": lineNumber,
						"end_line":    lineNumber,
						"lang":        "dart",
					}
					addDartRootKind(item, dartClassRootKinds(matches[1], publicLibraryPath)...)
					shared.AppendBucket(payload, "classes", item)
					pendingAnnotations = nil
				}
			}
		}
		if currentClass != nil {
			if item, ok := dartConstructorItem(trimmed, currentClass, lineNumber, rawLine, options); ok {
				shared.AppendBucket(payload, "functions", item)
				pendingAnnotations = nil
				updateDartClassScope(currentClass, trimmed)
				if currentClass.braceDepth <= 0 {
					currentClass = nil
				}
				continue
			}
		}
		if matches := dartFunctionPattern.FindStringSubmatch(trimmed); len(matches) == 3 {
			name := matches[1]
			switch name {
			case "if", "for", "while", "switch":
				continue
			}
			item := map[string]any{
				"name":        name,
				"line_number": lineNumber,
				"end_line":    lineNumber,
				"lang":        "dart",
				"decorators":  []string{},
			}
			if currentClass != nil {
				item["class_context"] = currentClass.name
			}
			decorators := append([]string(nil), pendingAnnotations...)
			if len(decorators) > 0 {
				item["decorators"] = decorators
			}
			addDartRootKind(item, dartFunctionRootKinds(
				name,
				currentClass,
				decorators,
				publicLibraryPath,
			)...)
			if options.IndexSource {
				item["source"] = rawLine
			}
			shared.AppendBucket(payload, "functions", item)
			pendingAnnotations = nil
		}
		if matches := dartVariablePattern.FindStringSubmatch(trimmed); len(matches) == 2 {
			name := matches[1]
			if _, ok := seenVariables[name]; !ok {
				seenVariables[name] = struct{}{}
				shared.AppendBucket(payload, "variables", map[string]any{
					"name":        name,
					"line_number": lineNumber,
					"end_line":    lineNumber,
					"lang":        "dart",
				})
			}
		}
		for _, match := range dartCallPattern.FindAllStringSubmatch(trimmed, -1) {
			if len(match) != 2 {
				continue
			}
			name := match[1]
			switch name {
			case "if", "for", "while", "switch":
				continue
			}
			appendUniqueRegexCall(payload, seenCalls, name, lineNumber, "dart")
		}
		if currentClass != nil {
			updateDartClassScope(currentClass, trimmed)
			if currentClass.braceDepth <= 0 {
				currentClass = nil
			}
		}
	}

	shared.SortNamedBucket(payload, "functions")
	shared.SortNamedBucket(payload, "classes")
	shared.SortNamedBucket(payload, "variables")
	shared.SortNamedBucket(payload, "imports")
	shared.SortNamedBucket(payload, "function_calls")
	return payload, nil
}

func dartConstructorItem(
	line string,
	scope *classScope,
	lineNumber int,
	rawLine string,
	options shared.Options,
) (map[string]any, bool) {
	if scope == nil {
		return nil, false
	}
	matches := scope.constructorPattern.FindStringSubmatch(line)
	if len(matches) == 0 {
		return nil, false
	}
	name := scope.name
	if len(matches) > 1 && matches[1] != "" {
		name += "." + matches[1]
	}
	item := map[string]any{
		"name":                 name,
		"line_number":          lineNumber,
		"end_line":             lineNumber,
		"lang":                 "dart",
		"class_context":        scope.name,
		"decorators":           []string{},
		"dead_code_root_kinds": []string{"dart.constructor"},
	}
	if options.IndexSource {
		item["source"] = rawLine
	}
	return item, true
}

func newDartClassScope(name string, extends string) *classScope {
	return &classScope{
		name:               name,
		extends:            extends,
		constructorPattern: regexp.MustCompile(`^\s*(?:const\s+|factory\s+)?` + regexp.QuoteMeta(name) + `(?:\.([A-Za-z_]\w*))?\s*\([^)]*\)\s*(?::|=>|\{|;)`),
	}
}

func dartClassRootKinds(name string, publicLibraryPath bool) []string {
	if !publicLibraryPath || strings.HasPrefix(name, "_") {
		return nil
	}
	return []string{"dart.public_library_api"}
}

func dartFunctionRootKinds(
	name string,
	scope *classScope,
	decorators []string,
	publicLibraryPath bool,
) []string {
	var kinds []string
	if scope == nil {
		if name == "main" {
			kinds = append(kinds, "dart.main_function")
		}
		if publicLibraryPath && !strings.HasPrefix(name, "_") && name != "main" {
			kinds = append(kinds, "dart.public_library_api")
		}
		return kinds
	}
	if slices.Contains(decorators, "@override") {
		kinds = append(kinds, "dart.override_method")
	}
	if name == "build" && (scope.extends == "StatelessWidget" || strings.HasPrefix(scope.extends, "State<")) {
		kinds = append(kinds, "dart.flutter_widget_build")
	}
	if name == "createState" && scope.extends == "StatefulWidget" {
		kinds = append(kinds, "dart.flutter_create_state")
	}
	if publicLibraryPath && !strings.HasPrefix(scope.name, "_") && !strings.HasPrefix(name, "_") {
		kinds = append(kinds, "dart.public_library_api")
	}
	return kinds
}

func addDartRootKind(item map[string]any, kinds ...string) {
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

func isPublicLibraryPath(path string) bool {
	slashed := filepath.ToSlash(path)
	return strings.Contains(slashed, "/lib/") && !strings.Contains(slashed, "/lib/src/")
}

func updateDartClassScope(scope *classScope, line string) {
	scope.braceDepth += strings.Count(line, "{")
	scope.braceDepth -= strings.Count(line, "}")
}

// PreScan returns Dart function and class names used by repository pre-scan.
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
